package clock

import (
	"sync"
	"testing"
	"time"
)

func TestClockSetIntervalFiresRepeatedly(t *testing.T) {
	c := New(5 * time.Millisecond)
	var mu sync.Mutex
	var n int
	c.SetInterval(5*time.Millisecond, func(Tick) {
		mu.Lock()
		n++
		mu.Unlock()
	})
	c.Start()
	defer c.Stop()

	deadline := time.After(500 * time.Millisecond)
	for {
		mu.Lock()
		got := n
		mu.Unlock()
		if got >= 3 {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for interval fires, got %d", got)
		case <-time.After(2 * time.Millisecond):
		}
	}
}

func TestClockSetTimeoutFiresOnce(t *testing.T) {
	c := New(5 * time.Millisecond)
	var mu sync.Mutex
	var n int
	c.SetTimeout(10*time.Millisecond, func(Tick) {
		mu.Lock()
		n++
		mu.Unlock()
	})
	c.Start()
	defer c.Stop()

	time.Sleep(100 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if n != 1 {
		t.Fatalf("SetTimeout fired %d times, want 1", n)
	}
}

func TestClockInstancePauseResume(t *testing.T) {
	c := New(5 * time.Millisecond)
	var mu sync.Mutex
	var n int
	inst := c.SetInterval(5*time.Millisecond, func(Tick) {
		mu.Lock()
		n++
		mu.Unlock()
	})
	c.Start()
	defer c.Stop()

	time.Sleep(30 * time.Millisecond)
	inst.Pause()
	mu.Lock()
	afterPause := n
	mu.Unlock()

	time.Sleep(30 * time.Millisecond)
	mu.Lock()
	stillPaused := n
	mu.Unlock()
	if stillPaused != afterPause {
		t.Fatalf("instance kept firing while paused: %d -> %d", afterPause, stillPaused)
	}

	inst.Resume()
	time.Sleep(30 * time.Millisecond)
	mu.Lock()
	afterResume := n
	mu.Unlock()
	if afterResume <= afterPause {
		t.Fatalf("instance did not resume firing: %d -> %d", afterPause, afterResume)
	}
}

func TestClockInstanceClear(t *testing.T) {
	c := New(5 * time.Millisecond)
	var mu sync.Mutex
	var n int
	inst := c.SetInterval(5*time.Millisecond, func(Tick) {
		mu.Lock()
		n++
		mu.Unlock()
	})
	c.Start()
	defer c.Stop()

	time.Sleep(20 * time.Millisecond)
	inst.Clear()
	if inst.Active() {
		t.Fatal("Active() = true after Clear()")
	}
	mu.Lock()
	afterClear := n
	mu.Unlock()

	time.Sleep(30 * time.Millisecond)
	mu.Lock()
	final := n
	mu.Unlock()
	if final != afterClear {
		t.Fatalf("cleared instance kept firing: %d -> %d", afterClear, final)
	}
}

func TestClockStopBeforeStart(t *testing.T) {
	c := New(5 * time.Millisecond)
	c.Stop() // must not panic
	c.Stop() // idempotent
}

func TestClockStopHaltsLoop(t *testing.T) {
	c := New(5 * time.Millisecond)
	var mu sync.Mutex
	var n int
	c.SetInterval(5*time.Millisecond, func(Tick) {
		mu.Lock()
		n++
		mu.Unlock()
	})
	c.Start()
	time.Sleep(20 * time.Millisecond)
	c.Stop()

	mu.Lock()
	after := n
	mu.Unlock()
	time.Sleep(30 * time.Millisecond)
	mu.Lock()
	final := n
	mu.Unlock()
	if final != after {
		t.Fatalf("clock kept running after Stop: %d -> %d", after, final)
	}
}

func TestClockElapsedAndDeltaTime(t *testing.T) {
	c := New(10 * time.Millisecond)
	if c.ElapsedTime() != 0 {
		t.Fatalf("ElapsedTime() before Start = %v, want 0", c.ElapsedTime())
	}
	if c.DeltaTime() != 10*time.Millisecond {
		t.Fatalf("DeltaTime() = %v, want 10ms", c.DeltaTime())
	}

	c.Start()
	defer c.Stop()
	time.Sleep(15 * time.Millisecond)
	if c.ElapsedTime() <= 0 {
		t.Fatal("expected positive ElapsedTime() after Start")
	}
}

func TestQuantizeRoundsToNearestTickMinOne(t *testing.T) {
	cases := []struct {
		d, interval time.Duration
		want        uint64
	}{
		{200 * time.Millisecond, 50 * time.Millisecond, 4},
		{1 * time.Millisecond, 50 * time.Millisecond, 1}, // clamped to minimum 1
		{120 * time.Millisecond, 50 * time.Millisecond, 2},
	}
	for _, tc := range cases {
		if got := quantize(tc.d, tc.interval); got != tc.want {
			t.Fatalf("quantize(%v, %v) = %d, want %d", tc.d, tc.interval, got, tc.want)
		}
	}
}
