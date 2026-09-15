package e2e_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/luciancaetano/knet"
	"github.com/luciancaetano/knet/internal/protocol"
	"github.com/luciancaetano/knet/ws"
)

// fakeMetrics is a minimal knet.Metrics that records counter totals.
type fakeMetrics struct {
	mu       sync.Mutex
	counters map[string]int
}

func newFakeMetrics() *fakeMetrics {
	return &fakeMetrics{counters: make(map[string]int)}
}

func (m *fakeMetrics) IncCounter(name string, tags ...string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.counters[name]++
}

func (m *fakeMetrics) ObserveHistogram(name string, value float64, tags ...string) {}
func (m *fakeMetrics) SetGauge(name string, value float64, tags ...string)         {}

func (m *fakeMetrics) count(name string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.counters[name]
}

const cmdPanic uint32 = 0x000A

// TestMetricsInstrumentation drives a real server through connect, a
// panicking handler, and a rate-limit rejection, and checks the expected
// knet.Metrics counters were incremented.
func TestMetricsInstrumentation(t *testing.T) {
	t.Parallel()

	metrics := newFakeMetrics()
	rateCfg := &ws.RateLimitConfig{MessagesPerSecond: 1, Burst: 1, Enabled: true}

	cfg := ws.NewConfig(":18086", rateCfg, ws.AllOrigins(), nil, nil)
	cfg = ws.WithMetrics(cfg, metrics)
	server := ws.New(cfg)
	ctx := context.Background()

	handled := make(chan struct{})
	server.RegisterHandler(ctx, cmdPanic, func(client knet.Client, payload []byte) {
		defer close(handled)
		panic("boom")
	})

	if err := server.Start(ctx); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		server.Stop(stopCtx)
	}()
	time.Sleep(200 * time.Millisecond)

	conn, _, err := newDialer().Dial("ws://localhost:18086/ws", nil)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close()

	if metrics.count("knet_conn_accepted_total") != 1 {
		t.Errorf("knet_conn_accepted_total = %d, want 1", metrics.count("knet_conn_accepted_total"))
	}

	encoded, _ := protocol.Encode(cmdPanic, nil)
	if err := conn.WriteMessage(websocket.BinaryMessage, encoded); err != nil {
		t.Fatalf("failed to send: %v", err)
	}
	select {
	case <-handled:
	case <-time.After(2 * time.Second):
		t.Fatal("panicking handler never ran")
	}
	time.Sleep(100 * time.Millisecond) // let dispatchHandler's deferred recover record the metric

	if metrics.count("knet_handler_panic_total") != 1 {
		t.Errorf("knet_handler_panic_total = %d, want 1", metrics.count("knet_handler_panic_total"))
	}

	// Burst is 1, so the second message in quick succession should be rate-limited.
	for i := 0; i < 3; i++ {
		encoded, _ := protocol.Encode(cmdPanic, nil)
		if err := conn.WriteMessage(websocket.BinaryMessage, encoded); err != nil {
			break
		}
	}
	time.Sleep(200 * time.Millisecond)

	if metrics.count("knet_ratelimit_rejected_total") < 1 {
		t.Errorf("knet_ratelimit_rejected_total = %d, want >= 1", metrics.count("knet_ratelimit_rejected_total"))
	}
}
