package timing

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/luciancaetano/knet/observer"
	"github.com/luciancaetano/knet/room"
)

func TestTimeManagerGlobalBroadcast(t *testing.T) {
	srv := &fakeServer{}
	tm := New(srv, 5*time.Millisecond)

	var mu sync.Mutex
	var ticks []uint64
	tm.Register(1, func(tick uint64) []byte {
		mu.Lock()
		ticks = append(ticks, tick)
		mu.Unlock()
		return []byte("payload")
	})

	ctx, cancel := context.WithCancel(context.Background())
	tm.Start(ctx)

	deadline := time.After(500 * time.Millisecond)
	for {
		mu.Lock()
		n := len(ticks)
		mu.Unlock()
		if n >= 2 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("timed out waiting for ticks")
		case <-time.After(2 * time.Millisecond):
		}
	}
	cancel()
	tm.Stop() // safe to call after ctx cancel
}

func TestTimeManagerRoomScopedBroadcast(t *testing.T) {
	srv := &fakeServer{}
	tm := New(srv, 5*time.Millisecond)
	rm := room.New("match")
	c := &fakeClient{id: "p1"}
	rm.Add(c)

	tm.RegisterRoom(rm, 2, func(tick uint64) []byte {
		return []byte("state")
	})

	ctx, cancel := context.WithCancel(context.Background())
	tm.Start(ctx)
	defer cancel()

	deadline := time.After(500 * time.Millisecond)
	for {
		if len(c.sent) >= 1 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("timed out waiting for room broadcast")
		case <-time.After(2 * time.Millisecond):
		}
	}
}

func TestTimeManagerSkipsNilPayload(t *testing.T) {
	srv := &fakeServer{}
	tm := New(srv, 5*time.Millisecond)
	tm.Register(1, func(tick uint64) []byte { return nil })

	ctx, cancel := context.WithCancel(context.Background())
	tm.Start(ctx)
	time.Sleep(30 * time.Millisecond)
	cancel()

	if len(srv.broadcasts) != 0 {
		t.Fatalf("expected no broadcasts for nil payload, got %d", len(srv.broadcasts))
	}
}

func TestTimeManagerStopBeforeStart(t *testing.T) {
	srv := &fakeServer{}
	tm := New(srv, 5*time.Millisecond)
	tm.Stop() // must not panic
	tm.Stop() // idempotent
}

func TestTimeManagerStopHaltsLoop(t *testing.T) {
	srv := &fakeServer{}
	tm := New(srv, 5*time.Millisecond)
	var mu sync.Mutex
	count := 0
	tm.Register(1, func(tick uint64) []byte {
		mu.Lock()
		count++
		mu.Unlock()
		return []byte("x")
	})

	tm.Start(context.Background())
	time.Sleep(20 * time.Millisecond)
	tm.Stop()

	mu.Lock()
	after := count
	mu.Unlock()
	time.Sleep(30 * time.Millisecond)
	mu.Lock()
	final := count
	mu.Unlock()

	if final != after {
		t.Fatalf("tm kept running after Stop: count went from %d to %d", after, final)
	}
}

func TestTimeManagerChaining(t *testing.T) {
	srv := &fakeServer{}
	tm := New(srv, time.Second)
	rm := room.New("r")
	result := tm.Register(1, func(uint64) []byte { return nil }).RegisterRoom(rm, 2, func(uint64) []byte { return nil })
	if result != tm {
		t.Fatal("expected chaining to return same tm")
	}
}

func TestTimeManagerPreAndPostTickHooks(t *testing.T) {
	srv := &fakeServer{}
	tm := New(srv, time.Second)

	var order []string
	tm.OnPreTick(func(tick uint64) { order = append(order, "pre") })
	tm.Register(1, func(tick uint64) []byte { order = append(order, "tick"); return nil })
	tm.OnPostTick(func(tick uint64) { order = append(order, "post") })

	tm.dispatch(context.Background(), 5)

	want := []string{"pre", "tick", "post"}
	if len(order) != len(want) {
		t.Fatalf("hook order = %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("hook order = %v, want %v", order, want)
		}
	}
}

func TestTimeManagerUptime(t *testing.T) {
	srv := &fakeServer{}
	tm := New(srv, time.Second)

	if tm.Uptime() != 0 {
		t.Fatalf("Uptime() before Start = %v, want 0", tm.Uptime())
	}

	ctx, cancel := context.WithCancel(context.Background())
	tm.Start(ctx)
	defer cancel()

	time.Sleep(5 * time.Millisecond)
	if tm.Uptime() <= 0 {
		t.Fatal("expected positive Uptime() after Start")
	}
}

func TestTimeManagerTickTimeConversions(t *testing.T) {
	srv := &fakeServer{}
	tm := New(srv, 50*time.Millisecond)

	if got, want := tm.TicksToTime(4), 200*time.Millisecond; got != want {
		t.Fatalf("TicksToTime(4) = %v, want %v", got, want)
	}
	if got, want := tm.TimeToTicks(120*time.Millisecond), uint64(2); got != want {
		t.Fatalf("TimeToTicks(120ms) = %d, want %d", got, want)
	}
	if got, want := tm.TimePassed(10, 4), 300*time.Millisecond; got != want {
		t.Fatalf("TimePassed(10, 4) = %v, want %v", got, want)
	}
	if got, want := tm.TimePassed(4, 10), -300*time.Millisecond; got != want {
		t.Fatalf("TimePassed(4, 10) = %v, want %v", got, want)
	}
}

func TestTimeManagerRegisterObserverSet(t *testing.T) {
	r := room.New("zone")
	near := &fakeClient{id: "near"}
	far := &fakeClient{id: "far"}
	r.Add(near)
	r.Add(far)

	positions := map[string][2]float64{"near": {0, 0}, "far": {100, 100}}
	cond := observer.DistanceCondition(
		func(clientID string) (float64, float64) { p := positions[clientID]; return p[0], p[1] },
		func(any) (float64, float64) { return 0, 0 },
		10,
	)
	set := observer.NewSet(r, cond)

	srv := &fakeServer{}
	tm := New(srv, 0)
	tm.RegisterObserverSet(set, nil, 1, func(uint64) []byte { return []byte("state") })

	tm.dispatch(context.Background(), 0)

	if len(near.sent) != 1 {
		t.Fatal("expected near client to receive dispatch")
	}
	if len(far.sent) != 0 {
		t.Fatal("expected far client to not receive dispatch")
	}
	if tm.CurrentTick() != 0 {
		t.Fatalf("CurrentTick() = %d, want 0", tm.CurrentTick())
	}
}
