// Package clock provides a fixed-rate, JS setInterval/setTimeout-style timer
// primitive for game loops: schedule repeating or one-shot callbacks against a
// single shared tick loop, pause/resume/reset individual timers, and read
// elapsed/delta/current time.
//
// Unlike a raw time.Ticker, callbacks receive a [Tick] snapshot (elapsed time,
// delta since last tick, wall-clock time, tick counter) and can be
// paused/resumed/cleared/reset individually via the [ClockInstance] handle
// returned from [Clock.SetInterval]/[Clock.SetTimeout].
//
// Example:
//
//	c := clock.New(50 * time.Millisecond) // 20 Hz base tick
//	c.Start()
//	defer c.Stop()
//
//	inst := c.SetInterval(200*time.Millisecond, func(t clock.Tick) {
//	    room.Broadcast(ctx, WorldStateCmd, world.Snapshot())
//	})
//	// later: inst.Pause() / inst.Resume() / inst.Clear()
package clock

import (
	"math"
	"sync"
	"sync/atomic"
	"time"
)

// Tick is a snapshot of clock state passed to every fired callback.
type Tick interface {
	// ElapsedTime returns time since the clock's Start call.
	ElapsedTime() time.Duration
	// DeltaTime returns the clock's fixed base tick interval.
	DeltaTime() time.Duration
	// CurrentTime returns wall-clock time at the moment of this tick.
	CurrentTime() time.Time
	// CurrentTick returns the monotonically increasing base-tick counter.
	CurrentTick() uint64
}

// tick is the concrete Tick implementation, immutable once built.
type tick struct {
	elapsed time.Duration
	delta   time.Duration
	current time.Time
	count   uint64
}

func (t tick) ElapsedTime() time.Duration { return t.elapsed }
func (t tick) DeltaTime() time.Duration   { return t.delta }
func (t tick) CurrentTime() time.Time     { return t.current }
func (t tick) CurrentTick() uint64        { return t.count }

// ClockInstance is a handle to a single SetInterval/SetTimeout registration.
type ClockInstance interface {
	// Pause stops fn from firing until Resume is called. No-op if cleared.
	Pause()
	// Resume re-enables firing after Pause. No-op if cleared.
	Resume()
	// Clear cancels the timer permanently; it will never fire again.
	Clear()
	// Reset restarts the countdown to the original duration, as if just
	// registered. Does not change the paused state.
	Reset()
	// ElapsedTime returns time since this instance was registered (or last Reset).
	ElapsedTime() time.Duration
	// Active reports whether the instance is still scheduled (not cleared).
	Active() bool
	// Paused reports whether the instance is currently paused.
	Paused() bool
}

// entryFn is called with the tick snapshot at fire time.
type entryFn func(t Tick)

// clockInstance is the concrete ClockInstance, owned/mutated by the clock's
// tick loop under the parent clock's lock.
type clockInstance struct {
	fn        entryFn
	period    uint64 // duration quantized to base ticks (>=1)
	remaining uint64 // base ticks left until next fire
	repeating bool   // true=SetInterval, false=SetTimeout (one-shot)
	active    bool
	paused    bool
	startedAt time.Time
	c         *clock
}

func (ci *clockInstance) Pause() {
	ci.c.mu.Lock()
	ci.paused = true
	ci.c.mu.Unlock()
}

func (ci *clockInstance) Resume() {
	ci.c.mu.Lock()
	ci.paused = false
	ci.c.mu.Unlock()
}

func (ci *clockInstance) Clear() {
	ci.c.mu.Lock()
	ci.active = false
	ci.c.mu.Unlock()
}

func (ci *clockInstance) Reset() {
	ci.c.mu.Lock()
	ci.remaining = ci.period
	ci.startedAt = time.Now()
	ci.c.mu.Unlock()
}

func (ci *clockInstance) ElapsedTime() time.Duration {
	ci.c.mu.Lock()
	defer ci.c.mu.Unlock()
	return time.Since(ci.startedAt)
}

func (ci *clockInstance) Active() bool {
	ci.c.mu.Lock()
	defer ci.c.mu.Unlock()
	return ci.active
}

func (ci *clockInstance) Paused() bool {
	ci.c.mu.Lock()
	defer ci.c.mu.Unlock()
	return ci.paused
}

// Clock drives a single fixed-rate tick loop that schedules SetInterval and
// SetTimeout callbacks. Timer durations are quantized to the clock's base
// interval (rounded to the nearest tick, minimum 1).
type Clock interface {
	// SetInterval schedules fn to fire repeatedly every d (quantized to the
	// base tick interval) until cleared.
	SetInterval(d time.Duration, fn func(t Tick)) ClockInstance
	// SetTimeout schedules fn to fire once after d (quantized to the base
	// tick interval), then auto-clears.
	SetTimeout(d time.Duration, fn func(t Tick)) ClockInstance
	// ClearInterval cancels a SetInterval registration. Equivalent to inst.Clear().
	ClearInterval(inst ClockInstance)
	// ClearTimeout cancels a SetTimeout registration. Equivalent to inst.Clear().
	ClearTimeout(inst ClockInstance)
	// Interval returns the fixed base tick interval this Clock runs at.
	Interval() time.Duration
	// ElapsedTime returns time since Start was called. Zero before Start.
	ElapsedTime() time.Duration
	// DeltaTime returns the base tick interval (constant, fixed-rate loop).
	DeltaTime() time.Duration
	// CurrentTime returns the current wall-clock time.
	CurrentTime() time.Time
	// CurrentTick returns the most recently dispatched base-tick counter.
	CurrentTick() uint64
	// Start launches the tick loop in a background goroutine. No-op if
	// already started.
	Start()
	// Stop halts the tick loop. Safe to call multiple times and before Start.
	Stop()
}

// clock is the default Clock implementation.
type clock struct {
	interval time.Duration

	mu      sync.Mutex
	entries []*clockInstance

	currentTick atomic.Uint64
	startedAt   atomic.Int64 // UnixNano; zero before Start

	started atomic.Bool
	stopCh  chan struct{}
	once    sync.Once
}

// New creates a Clock with the given fixed base tick interval. The clock does
// not run until [Clock.Start] is called.
func New(interval time.Duration) Clock {
	return &clock{
		interval: interval,
		stopCh:   make(chan struct{}),
	}
}

func quantize(d, interval time.Duration) uint64 {
	if interval <= 0 {
		return 1
	}
	n := uint64(math.Round(float64(d) / float64(interval)))
	if n < 1 {
		n = 1
	}
	return n
}

func (c *clock) schedule(d time.Duration, fn func(t Tick), repeating bool) ClockInstance {
	period := quantize(d, c.interval)
	ci := &clockInstance{
		fn:        fn,
		period:    period,
		remaining: period,
		repeating: repeating,
		active:    true,
		startedAt: time.Now(),
		c:         c,
	}
	c.mu.Lock()
	c.entries = append(c.entries, ci)
	c.mu.Unlock()
	return ci
}

func (c *clock) SetInterval(d time.Duration, fn func(t Tick)) ClockInstance {
	return c.schedule(d, fn, true)
}

func (c *clock) SetTimeout(d time.Duration, fn func(t Tick)) ClockInstance {
	return c.schedule(d, fn, false)
}

func (c *clock) ClearInterval(inst ClockInstance) { inst.Clear() }
func (c *clock) ClearTimeout(inst ClockInstance)  { inst.Clear() }

func (c *clock) Interval() time.Duration { return c.interval }

func (c *clock) ElapsedTime() time.Duration {
	start := c.startedAt.Load()
	if start == 0 {
		return 0
	}
	return time.Since(time.Unix(0, start))
}

func (c *clock) DeltaTime() time.Duration { return c.interval }
func (c *clock) CurrentTime() time.Time   { return time.Now() }
func (c *clock) CurrentTick() uint64      { return c.currentTick.Load() }

func (c *clock) Start() {
	if !c.started.CompareAndSwap(false, true) {
		return
	}
	c.startedAt.Store(time.Now().UnixNano())
	go c.run()
}

func (c *clock) Stop() {
	c.once.Do(func() { close(c.stopCh) })
}

func (c *clock) run() {
	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()

	var count uint64
	for {
		select {
		case <-ticker.C:
			c.dispatch(count)
			count++
		case <-c.stopCh:
			return
		}
	}
}

// dispatch fires every due, active, unpaused entry and prunes cleared ones.
// A snapshot is taken under the lock so SetInterval/SetTimeout/Clear calls
// from other goroutines cannot race with the dispatch loop; fn calls run
// sequentially outside the lock.
func (c *clock) dispatch(count uint64) {
	c.currentTick.Store(count)
	t := tick{
		elapsed: c.ElapsedTime(),
		delta:   c.interval,
		current: time.Now(),
		count:   count,
	}

	c.mu.Lock()
	due := make([]*clockInstance, 0, len(c.entries))
	kept := c.entries[:0]
	for _, ci := range c.entries {
		if !ci.active {
			continue // drop cleared entries
		}
		if !ci.paused {
			if ci.remaining == 0 {
				ci.remaining = ci.period
			}
			ci.remaining--
			if ci.remaining == 0 {
				due = append(due, ci)
			}
		}
		kept = append(kept, ci)
	}
	c.entries = kept
	c.mu.Unlock()

	for _, ci := range due {
		ci.fn(t)
		if !ci.repeating {
			ci.Clear()
		}
	}
}
