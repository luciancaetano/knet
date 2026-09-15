package bench

import (
	"fmt"
	"math"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Result is one CSV row: a scenario measured at one connection count.
type Result struct {
	Scenario       string
	Connections    int
	P50Ms          float64
	P95Ms          float64
	P99Ms          float64
	ThroughputMsgS float64
	BytesPerConn   float64
	TicksMeasured  uint64
	JitterMs       float64
}

// DefaultLoads is the connection-count matrix every scenario sweeps, unless
// overridden by the BENCH_LOADS env var (comma-separated ints) for fast local
// smoke runs, e.g. BENCH_LOADS=100,500.
func DefaultLoads() []int {
	if raw := os.Getenv("BENCH_LOADS"); raw != "" {
		var loads []int
		for _, part := range strings.Split(raw, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			n, err := strconv.Atoi(part)
			if err == nil && n > 0 {
				loads = append(loads, n)
			}
		}
		if len(loads) > 0 {
			return loads
		}
	}
	return []int{100, 500, 1000, 5000, 6000, 7000, 8000, 9000, 10000}
}

const (
	latencySampleCap  = 200              // clients sampled for RTT, bounded so large runs stay fast
	throughputWindow  = 3 * time.Second  // sustained send window for throughput measurement
	connectSettleWait = 30 * time.Second // max time to wait for all dials to register server-side
	echoTimeout       = 5 * time.Second
)

// runScenario drives one (withRoom, withTicker) combination across every load
// in loads, returning one Result per load.
func RunScenario(name string, withRoom, withTicker bool, loads []int) ([]Result, error) {
	results := make([]Result, 0, len(loads))
	for _, n := range loads {
		r, err := runOneLoad(name, withRoom, withTicker, n)
		if err != nil {
			return results, fmt.Errorf("%s @ %d connections: %w", name, n, err)
		}
		results = append(results, r)
	}
	return results, nil
}

func runOneLoad(name string, withRoom, withTicker bool, n int) (Result, error) {
	handle, err := startServer(withRoom, withTicker, 50*time.Millisecond)
	if err != nil {
		return Result{}, err
	}
	defer handle.Stop()

	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)

	clients, err := connectClients(handle.Addr, n)
	if err != nil {
		return Result{}, err
	}
	defer closeClients(clients)

	if err := waitForConnections(handle, n, connectSettleWait); err != nil {
		return Result{}, err
	}

	runtime.GC()
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	bytesPerConn := 0.0
	if after.HeapAlloc > before.HeapAlloc {
		bytesPerConn = float64(after.HeapAlloc-before.HeapAlloc) / float64(n)
	}

	p50, p95, p99 := sampleLatency(clients)

	var ticksMeasured uint64
	if handle.Timing != nil {
		ticksMeasured = handle.Timing.CurrentTick()
	}

	throughput := sampleThroughput(handle, clients)

	if handle.Timing != nil {
		ticksMeasured = handle.Timing.CurrentTick() - ticksMeasured
	}

	return Result{
		Scenario:       name,
		Connections:    n,
		P50Ms:          p50,
		P95Ms:          p95,
		P99Ms:          p99,
		ThroughputMsgS: throughput,
		BytesPerConn:   bytesPerConn,
		TicksMeasured:  ticksMeasured,
	}, nil
}

func waitForConnections(handle *serverHandle, n int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if handle.Metrics.ActiveConnections() >= int64(n) {
			return nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for %d active connections (got %d)", n, handle.Metrics.ActiveConnections())
}

// sampleLatency sends one echo per sampled client, sequentially, so queuing
// delay from concurrent sends doesn't inflate the measurement.
func sampleLatency(clients []*benchClient) (p50, p95, p99 float64) {
	sample := clients
	if len(sample) > latencySampleCap {
		sample = sample[:latencySampleCap]
	}

	samples := make([]float64, 0, len(sample))
	payload := []byte("bench")
	for _, c := range sample {
		start := time.Now()
		if err := c.sendEcho(payload); err != nil {
			continue
		}
		select {
		case t := <-c.lastEcho:
			samples = append(samples, t.Sub(start).Seconds()*1000)
		case <-time.After(echoTimeout):
		}
	}

	if len(samples) == 0 {
		return 0, 0, 0
	}
	sort.Float64s(samples)
	return percentile(samples, 0.50), percentile(samples, 0.95), percentile(samples, 0.99)
}

func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 1 {
		return sorted[0]
	}
	idx := int(math.Ceil(p*float64(len(sorted)))) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

// sampleThroughput has every client hammer the echo handler for
// throughputWindow and reports messages/sec measured via the server's own
// knet_msg_received_total counter (real lib metric, not a client-side tally).
func sampleThroughput(handle *serverHandle, clients []*benchClient) float64 {
	before := handle.Metrics.MessagesReceived()

	stop := make(chan struct{})
	var wg sync.WaitGroup
	var sent atomic.Int64
	payload := []byte("bench-throughput")

	for _, c := range clients {
		wg.Add(1)
		go func(c *benchClient) {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				if c.sendEcho(payload) == nil {
					sent.Add(1)
				}
				select {
				case <-c.lastEcho:
				case <-time.After(50 * time.Millisecond):
				}
			}
		}(c)
	}

	time.Sleep(throughputWindow)
	close(stop)
	wg.Wait()

	after := handle.Metrics.MessagesReceived()
	return float64(after-before) / throughputWindow.Seconds()
}
