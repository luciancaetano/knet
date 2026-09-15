package bench

import (
	"context"
	"math"
	"net"
	"sort"
	"sync"
	"time"

	"github.com/luciancaetano/knet"
	"github.com/luciancaetano/knet/internal/room"
	"github.com/luciancaetano/knet/timing"
	"github.com/luciancaetano/knet/ws"
)

// JitterResult is one CSV row for the ticker jitter test.
type JitterResult struct {
	Connections        int
	Mode               string // "no_room" or "room"
	ExpectedIntervalMs float64
	MeanDriftMs        float64
	P99DriftMs         float64
	MaxDriftMs         float64
}

const jitterMeasureDuration = 5 * time.Second

// measureTickerJitter starts a server with a Ticker (RoomManager-scoped when
// withRoom is true) under n connected clients, records the actual firing time
// of every tick via OnPreTick, and reports how far each interval drifted from
// the configured interval.
func MeasureTickerJitter(withRoom bool, n int, interval time.Duration) (JitterResult, error) {
	mode := "no_room"
	if withRoom {
		mode = "room"
	}

	var rm room.Room
	if withRoom {
		rm = room.New("jitter-bench")
	}

	metrics := newCollectorMetrics()
	addr, err := freePort()
	if err != nil {
		return JitterResult{}, err
	}

	cfg := ws.NewConfig(addr, ws.NoRateLimit(), ws.AllOrigins(), func(c knet.Client) bool {
		if rm != nil {
			rm.Add(c)
		}
		return true
	}, func(c knet.Client, _ bool) {
		if rm != nil {
			rm.Remove(c.ID())
		}
	})
	cfg.Metrics = metrics
	server := ws.New(cfg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := server.RegisterHandler(ctx, EchoCommandID, func(c knet.Client, payload []byte) {
		_ = c.Send(ctx, EchoCommandID, payload)
	}); err != nil {
		return JitterResult{}, err
	}

	tm := timing.New(server, interval)

	var mu sync.Mutex
	var fireTimes []time.Time
	tm.OnPreTick(func(uint64) {
		mu.Lock()
		fireTimes = append(fireTimes, time.Now())
		mu.Unlock()
	})

	payload := []byte("jitter")
	if rm != nil {
		tm.RegisterRoom(rm, 2, func(uint64) []byte { return payload })
	} else {
		tm.Register(2, func(uint64) []byte { return payload })
	}

	go func() { _ = server.Start(ctx) }()
	time.Sleep(50 * time.Millisecond)

	clients, err := connectClients(addr, n)
	if err != nil {
		return JitterResult{}, err
	}
	defer closeClients(clients)

	if err := waitForConnections(&serverHandle{Metrics: metrics}, n, connectSettleWait); err != nil {
		return JitterResult{}, err
	}

	tm.Start(ctx)
	time.Sleep(jitterMeasureDuration)
	tm.Stop()

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	_ = server.Stop(stopCtx)
	stopCancel()

	mu.Lock()
	times := append([]time.Time(nil), fireTimes...)
	mu.Unlock()

	if len(times) < 2 {
		return JitterResult{}, nil
	}

	drifts := make([]float64, 0, len(times)-1)
	for i := 1; i < len(times); i++ {
		actual := times[i].Sub(times[i-1])
		drift := math.Abs((actual - interval).Seconds() * 1000)
		drifts = append(drifts, drift)
	}
	sort.Float64s(drifts)

	var sum float64
	for _, d := range drifts {
		sum += d
	}

	return JitterResult{
		Connections:        n,
		Mode:               mode,
		ExpectedIntervalMs: interval.Seconds() * 1000,
		MeanDriftMs:        sum / float64(len(drifts)),
		P99DriftMs:         percentile(drifts, 0.99),
		MaxDriftMs:         drifts[len(drifts)-1],
	}, nil
}

func freePort() (string, error) {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	addr := lis.Addr().String()
	lis.Close() //nolint:errcheck // just reserving a free port
	return addr, nil
}
