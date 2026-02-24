package knet

import (
	"context"
	"sync"
	"time"
)

// TickFn is called once per tick by [Ticker].
//
// tick is a monotonically increasing counter starting at zero on the first
// tick. Returning nil skips the broadcast for that tick, which is useful for
// delta-update strategies where nothing has changed since the last tick.
type TickFn func(tick uint64) []byte

// Ticker drives fixed-rate state broadcasts for game loops.
//
// It decouples your simulation frequency from the network update rate: game
// logic can run at any speed while Ticker broadcasts snapshots at a predictable
// cadence (commonly 20 Hz = 50 ms for multiplayer games).
//
// Multiple handlers can be registered on a single Ticker — each receives its
// own independent TickFn call on every tick. Handlers can be scoped to a
// specific [Room] (e.g. a match) or broadcast globally to all connected clients.
//
// Example — 20 Hz world-state broadcast:
//
//	ticker := knet.NewTicker(server, 50*time.Millisecond)
//
//	// Global broadcast: send world state to every connected client
//	ticker.Register(WorldStateCmd, func(tick uint64) []byte {
//	    return world.Snapshot()
//	})
//
//	// Room-scoped broadcast: send positions only to players in the match
//	ticker.RegisterRoom(matchRoom, PlayerPosCmd, func(tick uint64) []byte {
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
//	ticker.Start(ctx) // non-blocking background goroutine
//	server.Start(ctx)
type Ticker struct {
	interval time.Duration
	server   Server

	mu      sync.Mutex
	entries []tickEntry

	stopCh chan struct{}
	once   sync.Once
}

// tickEntry is a single registered broadcast handler.
type tickEntry struct {
	commandID uint32
	fn        TickFn
	room      Room // nil → global broadcast via server.BroadcastCommand
}

// NewTicker creates a Ticker that fires at the given interval.
//
// interval is typically between 50 ms (20 Hz) and 100 ms (10 Hz).
// Call [Ticker.Register] or [Ticker.RegisterRoom] to add handlers, then
// [Ticker.Start] to begin the loop.
func NewTicker(srv Server, interval time.Duration) *Ticker {
	return &Ticker{
		interval: interval,
		server:   srv,
		stopCh:   make(chan struct{}),
	}
}

// Register adds a global-broadcast handler to the Ticker.
//
// On each tick, fn is called with the current tick counter. If fn returns nil
// the broadcast is skipped for that tick. Returns the Ticker for chaining.
func (t *Ticker) Register(commandID uint32, fn TickFn) *Ticker {
	t.mu.Lock()
	t.entries = append(t.entries, tickEntry{commandID: commandID, fn: fn})
	t.mu.Unlock()
	return t
}

// RegisterRoom adds a room-scoped broadcast handler to the Ticker.
//
// On each tick, fn is called and the result (if non-nil) is broadcast only to
// the clients in room. Returns the Ticker for chaining.
func (t *Ticker) RegisterRoom(room Room, commandID uint32, fn TickFn) *Ticker {
	t.mu.Lock()
	t.entries = append(t.entries, tickEntry{commandID: commandID, fn: fn, room: room})
	t.mu.Unlock()
	return t
}

// Start launches the tick loop in a background goroutine.
//
// The loop exits when ctx is cancelled or [Ticker.Stop] is called.
// Start is non-blocking; use a blocking call (e.g. signal wait or
// [Server.Start] with a long-lived context) to keep the process alive.
func (t *Ticker) Start(ctx context.Context) {
	go t.run(ctx)
}

// Stop halts the tick loop. Safe to call multiple times and before Start.
func (t *Ticker) Stop() {
	t.once.Do(func() { close(t.stopCh) })
}

func (t *Ticker) run(ctx context.Context) {
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

// dispatch calls every registered TickFn and broadcasts non-nil payloads.
// A snapshot of entries is taken under the lock so that Register/RegisterRoom
// calls from other goroutines cannot race with the dispatch loop.
func (t *Ticker) dispatch(ctx context.Context, tick uint64) {
	t.mu.Lock()
	entries := make([]tickEntry, len(t.entries))
	copy(entries, t.entries)
	t.mu.Unlock()

	for _, e := range entries {
		payload := e.fn(tick)
		if payload == nil {
			continue
		}
		if e.room != nil {
			e.room.Broadcast(ctx, e.commandID, payload) //nolint:errcheck
		} else {
			t.server.BroadcastCommand(ctx, e.commandID, payload) //nolint:errcheck
		}
	}
}
