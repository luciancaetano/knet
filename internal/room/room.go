package room

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/luciancaetano/knet"
	"github.com/luciancaetano/knet/clock"
)

// Room is a named set of connected clients that can be broadcast to as a group.
//
// The primary use-case is game server primitives such as match instances,
// lobbies, zones, and chat channels. All methods are safe for concurrent use.
//
// Example — broadcast to everyone in a lobby except the sender:
//
//	lobby := room.New("lobby-1", 50*time.Millisecond)
//
//	// OnConnect: track the client
//	func(c knet.Client) bool {
//	    lobby.Add(c)
//	    return true
//	}
//
//	// OnDisconnect: clean up
//	func(c knet.Client, voluntary bool) {
//	    lobby.Remove(c.ID())
//	}
//
//	// Handler: relay chat to everyone else
//	func handleChat(client knet.Client, payload []byte) {
//	    lobby.BroadcastExcept(ctx, client.ID(), ChatCmd, payload)
//	}
//
// View is the read/broadcast facade of a Room — everything a consumer that
// only observes or messages a room needs (e.g. [roommanager.Manager.Room],
// [github.com/luciancaetano/knet/observer.NewSet],
// [github.com/luciancaetano/knet/timing.TimeManager.RegisterRoom]).
// It deliberately excludes membership mutation (Add/Remove/Close) — those
// stay on [Room], owned by whoever manages the room's lifecycle.
type View interface {
	// ID returns the room's identifier as passed to [New].
	ID() string

	// Has reports whether a client with the given ID is in the room.
	Has(clientID string) bool

	// Clients returns a point-in-time snapshot of all clients in the room.
	Clients() []knet.Client

	// Size returns the number of clients currently in the room.
	Size() int

	// Broadcast sends commandID + payload to every client in the room.
	// Clients whose send buffers are full or whose connections are closed are
	// skipped; the returned error reports how many deliveries failed.
	Broadcast(ctx context.Context, commandID uint32, payload []byte) error

	// BroadcastExcept sends to every client except the one with excludeID.
	// Typically used in relay patterns where the sender should not receive
	// their own message.
	BroadcastExcept(ctx context.Context, excludeID string, commandID uint32, payload []byte) error

	// Clock returns this room's own [clock.Clock], for scheduling
	// SetInterval/SetTimeout callbacks scoped to the room (e.g. a match tick
	// loop). It is created stopped; call Clock().Start() to run it.
	Clock() clock.Clock
}

// Room is the full room primitive, adding membership mutation on top of
// [View]. Reach for [View] in any signature that only needs to read or
// broadcast — reserve Room for the code that actually owns join/leave
// lifecycle (typically [roommanager.Manager] or a hand-rolled onConnect
// wiring, see [New]).
type Room interface {
	View

	// Add inserts a client into the room. If a client with the same ID is
	// already present it is replaced (idempotent on reconnect).
	Add(client knet.Client)

	// Remove evicts the client with the given ID. No-op if not present.
	Remove(clientID string)

	// Close removes all clients from the room and closes their connections.
	// Use this to terminate a match or lobby and disconnect every participant.
	Close(ctx context.Context)
}

// room is the default Room implementation.
type room struct {
	id      string
	mu      sync.RWMutex
	clients map[string]knet.Client
	clock   clock.Clock
}

// New creates a new, empty Room with the given identifier and a stopped
// [clock.Clock] running at tickInterval, available via [Room.Clock].
// The id is arbitrary; the caller is responsible for uniqueness within
// the application.
func New(id string, tickInterval time.Duration) Room {
	return &room{
		id:      id,
		clients: make(map[string]knet.Client),
		clock:   clock.New(tickInterval),
	}
}

func (r *room) ID() string { return r.id }

func (r *room) Clock() clock.Clock { return r.clock }

func (r *room) Add(client knet.Client) {
	r.mu.Lock()
	r.clients[client.ID()] = client
	r.mu.Unlock()
}

func (r *room) Remove(clientID string) {
	r.mu.Lock()
	delete(r.clients, clientID)
	r.mu.Unlock()
}

func (r *room) Has(clientID string) bool {
	r.mu.RLock()
	_, ok := r.clients[clientID]
	r.mu.RUnlock()
	return ok
}

func (r *room) Clients() []knet.Client {
	r.mu.RLock()
	out := make([]knet.Client, 0, len(r.clients))
	for _, c := range r.clients {
		out = append(out, c)
	}
	r.mu.RUnlock()
	return out
}

func (r *room) Size() int {
	r.mu.RLock()
	n := len(r.clients)
	r.mu.RUnlock()
	return n
}

func (r *room) Broadcast(ctx context.Context, commandID uint32, payload []byte) error {
	return r.broadcastFiltered(ctx, "", commandID, payload)
}

func (r *room) BroadcastExcept(ctx context.Context, excludeID string, commandID uint32, payload []byte) error {
	return r.broadcastFiltered(ctx, excludeID, commandID, payload)
}

// maxBroadcastConcurrency bounds how many Send calls a single broadcast runs
// at once, so a room with thousands of members can't fork thousands of
// goroutines per tick (DoS via unbounded fan-out).
const maxBroadcastConcurrency = 64

// perClientSendTimeout bounds how long a broadcast waits on any one client's
// send buffer, so a stalled client can't hold a concurrency slot forever.
const perClientSendTimeout = 2 * time.Second

// broadcastFiltered sends to all clients except excludeID (empty = send to all).
// The client map is snapshotted under the read lock; sending happens outside
// the lock so slow clients cannot stall room operations.
//
// Deliveries run concurrently, bounded by a semaphore (maxBroadcastConcurrency)
// so a single slow or blocked client delays only its own delivery without
// letting the number of in-flight goroutines grow with room size.
func (r *room) broadcastFiltered(ctx context.Context, excludeID string, commandID uint32, payload []byte) error {
	r.mu.RLock()
	targets := make([]knet.Client, 0, len(r.clients))
	for id, c := range r.clients {
		if id != excludeID {
			targets = append(targets, c)
		}
	}
	r.mu.RUnlock()

	sem := make(chan struct{}, maxBroadcastConcurrency)
	var failCount atomic.Int64
	var wg sync.WaitGroup
	wg.Add(len(targets))
	for _, c := range targets {
		sem <- struct{}{}
		go func(c knet.Client) {
			defer wg.Done()
			defer func() { <-sem }()

			sendCtx, cancel := context.WithTimeout(ctx, perClientSendTimeout)
			defer cancel()
			if err := c.Send(sendCtx, commandID, payload); err != nil {
				failCount.Add(1)
			}
		}(c)
	}
	wg.Wait()

	if n := failCount.Load(); n > 0 {
		return fmt.Errorf("room %s: failed to deliver to %d client(s)", r.id, n)
	}
	return nil
}

func (r *room) Close(ctx context.Context) {
	r.clock.Stop()

	r.mu.Lock()
	clients := make([]knet.Client, 0, len(r.clients))
	for _, c := range r.clients {
		clients = append(clients, c)
	}
	r.clients = make(map[string]knet.Client)
	r.mu.Unlock()

	for _, c := range clients {
		c.Close(ctx) //nolint:errcheck
	}
}
