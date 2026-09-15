//go:build docker

// Stress test against a knet server running in a Docker container (real
// network stack, isolated process), as opposed to the other tests in this
// package which start the server in-process.
//
// Requires the container from docker-compose.yml to be up and reachable at
// KNET_STRESS_ADDR (default localhost:9000):
//
//	docker compose up -d --build
//	cd tests/stress && go test -tags docker -run TestDockerStress -timeout 5m -v
package stress_test

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/luciancaetano/knet/internal/protocol"
)

const echoCommand = 0x0001

func dockerStressAddr() string {
	if a := os.Getenv("KNET_STRESS_ADDR"); a != "" {
		return a
	}
	return "localhost:9000"
}

func encodeEcho(payload []byte) []byte {
	frame, err := protocol.Encode(echoCommand, payload)
	if err != nil {
		panic(err)
	}
	return frame
}

func dialEcho(t *testing.T, addr string) *websocket.Conn {
	t.Helper()
	url := fmt.Sprintf("ws://%s/ws", addr)
	conn, _, err := websocket.DefaultDialer.DialContext(context.Background(), url, nil)
	if err != nil {
		t.Fatalf("dial %s: %v", url, err)
	}
	return conn
}

// TestDockerStressThroughput connects N concurrent clients to the
// containerized server, round-trips a message on each, and reports
// throughput/latency.
func TestDockerStressThroughput(t *testing.T) {
	addr := dockerStressAddr()
	const numClients = 300

	var (
		connected, failed, sent, received, totalLatencyUs int64
		wg                                                 sync.WaitGroup
	)

	start := time.Now()
	for i := 0; i < numClients; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			conn, _, err := websocket.DefaultDialer.DialContext(context.Background(), fmt.Sprintf("ws://%s/ws", addr), nil)
			if err != nil {
				atomic.AddInt64(&failed, 1)
				return
			}
			defer conn.Close()
			atomic.AddInt64(&connected, 1)

			conn.SetReadDeadline(time.Now().Add(10 * time.Second))
			sendStart := time.Now()
			if err := conn.WriteMessage(websocket.BinaryMessage, encodeEcho([]byte(fmt.Sprintf("hello-%d", id)))); err != nil {
				return
			}
			atomic.AddInt64(&sent, 1)

			if _, _, err := conn.ReadMessage(); err == nil {
				atomic.AddInt64(&received, 1)
				atomic.AddInt64(&totalLatencyUs, time.Since(sendStart).Microseconds())
			}
		}(i)
	}
	wg.Wait()
	duration := time.Since(start)

	avgLatency := int64(0)
	if received > 0 {
		avgLatency = totalLatencyUs / received
	}

	t.Logf("connected=%d/%d failed=%d sent=%d received=%d avg_latency=%dus duration=%v",
		connected, numClients, failed, sent, received, avgLatency, duration)

	if connected < int64(numClients*0.95) {
		t.Errorf("too many failed connections: %d/%d", connected, numClients)
	}
	if received < sent*95/100 {
		t.Errorf("too many lost echoes: sent=%d received=%d", sent, received)
	}
}

// TestDockerStressReconnect opens and closes connections in bursts against
// the containerized server and does a coarse goroutine-leak check: the
// process' own goroutine count should settle back down after the clients
// disconnect (this only catches leaks in the test client itself, but the
// containerized server is also checked implicitly by still answering health
// checks / accepting new connections afterwards).
func TestDockerStressReconnect(t *testing.T) {
	addr := dockerStressAddr()
	const rounds = 5
	const clientsPerRound = 100

	before := runtime.NumGoroutine()

	for r := 0; r < rounds; r++ {
		var wg sync.WaitGroup
		for i := 0; i < clientsPerRound; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				conn := dialEcho(t, addr)
				conn.WriteMessage(websocket.BinaryMessage, encodeEcho([]byte("ping")))
				conn.SetReadDeadline(time.Now().Add(5 * time.Second))
				conn.ReadMessage()
				conn.Close()
			}()
		}
		wg.Wait()
	}

	// Let goroutines/connections wind down.
	time.Sleep(1 * time.Second)
	runtime.GC()
	after := runtime.NumGoroutine()

	t.Logf("goroutines before=%d after=%d", before, after)
	if after > before+20 {
		t.Errorf("possible goroutine leak in client: before=%d after=%d", before, after)
	}

	// Server should still accept new connections after the churn.
	conn := dialEcho(t, addr)
	defer conn.Close()
	if err := conn.WriteMessage(websocket.BinaryMessage, encodeEcho([]byte("still-alive"))); err != nil {
		t.Fatalf("server not responsive after reconnect churn: %v", err)
	}
}
