package timing

import (
	"context"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/luciancaetano/knet"
	"github.com/luciancaetano/knet/observer"
	"github.com/luciancaetano/knet/room"
)

// TickFn is called once per tick by [TimeManager].
//
// tick is a monotonically increasing counter starting at zero on the first
// tick. Returning nil skips the broadcast for that tick, which is useful for
// delta-update strategies where nothing has changed since the last tick.
type TickFn func(tick uint64) []byte

// TickHookFn is called once per tick by [TimeManager.OnPreTick] or
// [TimeManager.OnPostTick] handlers. Unlike [TickFn] it has no return value —
// it's for side effects (e.g. syncing external simulation state), not
// broadcasting.
type TickHookFn func(tick uint64)

// TimeManager drives fixed-rate state broadcasts and tracks tick-relative
// timing for game loops (tick lifecycle, uptime, tick↔duration conversions,
// and per-client round-trip time).
//
// It decouples your simulation frequency from the network update rate: game
// logic can run at any speed while TimeManager broadcasts snapshots at a
// predictable cadence (commonly 20 Hz = 50 ms for multiplayer games).
//
// Multiple handlers can be registered on a single TimeManager — each receives
// its own independent TickFn call on every tick. Handlers can be scoped to a
// specific [Room] (e.g. a match) or broadcast globally to all connected
// clients.
//
// Example — 20 Hz world-state broadcast:
//
//	tm := timing.New(server, 50*time.Millisecond)
//
//	// Global broadcast: send world state to every connected client
//	tm.Register(WorldStateCmd, func(tick uint64) []byte {
//	    return world.Snapshot()
//	})
//
//	// Room-scoped broadcast: send positions only to players in the match
//	tm.RegisterRoom(matchRoom, PlayerPosCmd, func(tick uint64) []byte {
//	    // Send a full snapshot every second (20 ticks), deltas in between
//	    if tick%20 == 0 {
//	        return match.FullPositions()
//	    }
//	    return match.DeltaPositions()
//	})
//
//	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
//	defer stop()
//
//	tm.Start(ctx) // non-blocking background goroutine
//	server.Start(ctx)
type TimeManager struct {
	interval time.Duration
	server   knet.Server

	mu        sync.Mutex
	entries   []tickEntry
	preHooks  []TickHookFn
	postHooks []TickHookFn

	currentTick atomic.Uint64
	startedAt   atomic.Int64 // UnixNano; zero before Start

	rttMu sync.RWMutex
	rtts  map[string]uint64 // clientID -> last measured RTT in ticks; see ping.go

	stopCh chan struct{}
	once   sync.Once
}

// tickEntry is a single registered broadcast handler.
type tickEntry struct {
	commandID uint32
	fn        TickFn
	room      room.Room     // nil → global broadcast via server.BroadcastCommand
	observers *observer.Set // if set, takes precedence over room for filtering recipients
	subject   any           // passed to observers.Broadcast when observers is set
}

// New creates a TimeManager that fires at the given interval.
//
// interval is typically between 50 ms (20 Hz) and 100 ms (10 Hz).
// Call [TimeManager.Register] or [TimeManager.RegisterRoom] to add handlers,
// then [TimeManager.Start] to begin the loop.
func New(srv knet.Server, interval time.Duration) *TimeManager {
	return &TimeManager{
		interval: interval,
		server:   srv,
		stopCh:   make(chan struct{}),
	}
}

// Register adds a global-broadcast handler to the TimeManager.
//
// On each tick, fn is called with the current tick counter. If fn returns nil
// the broadcast is skipped for that tick. Returns the TimeManager for chaining.
func (t *TimeManager) Register(commandID uint32, fn TickFn) *TimeManager {
	t.mu.Lock()
	t.entries = append(t.entries, tickEntry{commandID: commandID, fn: fn})
	t.mu.Unlock()
	return t
}

// RegisterRoom adds a room-scoped broadcast handler to the TimeManager.
//
// On each tick, fn is called and the result (if non-nil) is broadcast only to
// the clients in room. Returns the TimeManager for chaining.
func (t *TimeManager) RegisterRoom(rm room.Room, commandID uint32, fn TickFn) *TimeManager {
	t.mu.Lock()
	t.entries = append(t.entries, tickEntry{commandID: commandID, fn: fn, room: rm})
	t.mu.Unlock()
	return t
}

// RegisterObserverSet adds an interest-filtered broadcast handler to the
// TimeManager.
//
// On each tick, fn is called and the result (if non-nil) is broadcast only to
// clients in observers.room that pass every condition in observers for subject
// (see [ObserverSet]). Use this instead of [TimeManager.RegisterRoom] when
// updates about subject should only reach clients that currently have
// interest in it (distance, ownership, scene, or any custom condition).
// Returns the TimeManager for chaining.
func (t *TimeManager) RegisterObserverSet(observers *observer.Set, subject any, commandID uint32, fn TickFn) *TimeManager {
	t.mu.Lock()
	t.entries = append(t.entries, tickEntry{commandID: commandID, fn: fn, observers: observers, subject: subject})
	t.mu.Unlock()
	return t
}

// OnPreTick registers fn to run before tick handlers dispatch on each tick.
// Use it to sync external simulation state (e.g. apply buffered input) right
// before broadcasts are computed. Returns the TimeManager for chaining.
func (t *TimeManager) OnPreTick(fn TickHookFn) *TimeManager {
	t.mu.Lock()
	t.preHooks = append(t.preHooks, fn)
	t.mu.Unlock()
	return t
}

// OnPostTick registers fn to run after tick handlers dispatch on each tick.
// Use it for cleanup (e.g. clear per-tick buffers) after broadcasts are sent.
// Returns the TimeManager for chaining.
func (t *TimeManager) OnPostTick(fn TickHookFn) *TimeManager {
	t.mu.Lock()
	t.postHooks = append(t.postHooks, fn)
	t.mu.Unlock()
	return t
}

// CurrentTick returns the tick number most recently dispatched.
//
// Useful for stamping application-level messages (e.g. player input, SyncVar
// updates) with the tick they belong to, so clients can order or reconcile
// them later.
func (t *TimeManager) CurrentTick() uint64 {
	return t.currentTick.Load()
}

// Interval returns the fixed tick interval this TimeManager runs at.
func (t *TimeManager) Interval() time.Duration {
	return t.interval
}

// Uptime returns how long this TimeManager has been running since [Start]
// was called. Returns zero before Start.
func (t *TimeManager) Uptime() time.Duration {
	start := t.startedAt.Load()
	if start == 0 {
		return 0
	}
	return time.Since(time.Unix(0, start))
}

// TicksToTime converts a tick count to the elapsed duration at this
// TimeManager's tick rate.
func (t *TimeManager) TicksToTime(ticks uint64) time.Duration {
	return time.Duration(ticks) * t.interval
}

// TimeToTicks converts a duration to the nearest number of ticks at this
// TimeManager's tick rate. Returns 0 if the tick interval is not positive.
func (t *TimeManager) TimeToTicks(d time.Duration) uint64 {
	if t.interval <= 0 {
		return 0
	}
	return uint64(math.Round(float64(d) / float64(t.interval)))
}

// TimePassed returns the elapsed duration between two tick numbers. If
// currentTick is before previousTick, the result is negative.
func (t *TimeManager) TimePassed(currentTick, previousTick uint64) time.Duration {
	return time.Duration(int64(currentTick)-int64(previousTick)) * t.interval
}

// Start launches the tick loop in a background goroutine.
//
// The loop exits when ctx is cancelled or [TimeManager.Stop] is called.
// Start is non-blocking; use a blocking call (e.g. signal wait or
// [Server.Start] with a long-lived context) to keep the process alive.
func (t *TimeManager) Start(ctx context.Context) {
	t.startedAt.Store(time.Now().UnixNano())
	go t.run(ctx)
}

// Stop halts the tick loop. Safe to call multiple times and before Start.
func (t *TimeManager) Stop() {
	t.once.Do(func() { close(t.stopCh) })
}

func (t *TimeManager) run(ctx context.Context) {
	ticker := time.NewTicker(t.interval)
	defer ticker.Stop()

	var tick uint64
	for {
		select {
		case <-ticker.C:
			t.dispatch(ctx, tick)
			tick++
		case <-ctx.Done():
			return
		case <-t.stopCh:
			return
		}
	}
}

// dispatch runs pre-tick hooks, calls every registered TickFn and broadcasts
// non-nil payloads, then runs post-tick hooks. A snapshot of entries/hooks is
// taken under the lock so that Register*/OnPreTick/OnPostTick calls from
// other goroutines cannot race with the dispatch loop.
func (t *TimeManager) dispatch(ctx context.Context, tick uint64) {
	t.currentTick.Store(tick)

	t.mu.Lock()
	entries := make([]tickEntry, len(t.entries))
	copy(entries, t.entries)
	preHooks := make([]TickHookFn, len(t.preHooks))
	copy(preHooks, t.preHooks)
	postHooks := make([]TickHookFn, len(t.postHooks))
	copy(postHooks, t.postHooks)
	t.mu.Unlock()

	for _, h := range preHooks {
		h(tick)
	}

	for _, e := range entries {
		payload := e.fn(tick)
		if payload == nil {
			continue
		}
		switch {
		case e.observers != nil:
			e.observers.Broadcast(ctx, e.subject, e.commandID, payload) //nolint:errcheck
		case e.room != nil:
			e.room.Broadcast(ctx, e.commandID, payload) //nolint:errcheck
		default:
			t.server.BroadcastCommand(ctx, e.commandID, payload) //nolint:errcheck
		}
	}

	for _, h := range postHooks {
		h(tick)
	}
}
