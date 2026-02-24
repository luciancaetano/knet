package knet

import (
	"context"
	"fmt"
	"sync"
)

// Room is a named set of connected clients that can be broadcast to as a group.
//
// The primary use-case is game server primitives such as match instances,
// lobbies, zones, and chat channels. All methods are safe for concurrent use.
//
// Example — broadcast to everyone in a lobby except the sender:
//
//	lobby := knet.NewRoom("lobby-1")
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
type Room interface {
	// ID returns the room's identifier as passed to [NewRoom].
	ID() string

	// Add inserts a client into the room. If a client with the same ID is
	// already present it is replaced (idempotent on reconnect).
	Add(client Client)

	// Remove evicts the client with the given ID. No-op if not present.
	Remove(clientID string)

	// Has reports whether a client with the given ID is in the room.
	Has(clientID string) bool

	// Clients returns a point-in-time snapshot of all clients in the room.
	Clients() []Client

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

	// Close removes all clients from the room and closes their connections.
	// Use this to terminate a match or lobby and disconnect every participant.
	Close(ctx context.Context)
}

// room is the default Room implementation.
type room struct {
	id      string
	mu      sync.RWMutex
	clients map[string]Client
}

// NewRoom creates a new, empty Room with the given identifier.
// The id is arbitrary; the caller is responsible for uniqueness within
// the application.
func NewRoom(id string) Room {
	return &room{
		id:      id,
		clients: make(map[string]Client),
	}
}

func (r *room) ID() string { return r.id }

func (r *room) Add(client Client) {
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

func (r *room) Clients() []Client {
	r.mu.RLock()
	out := make([]Client, 0, len(r.clients))
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

// broadcastFiltered sends to all clients except excludeID (empty = send to all).
// The client map is snapshotted under the read lock; sending happens outside
// the lock so slow clients cannot stall room operations.
func (r *room) broadcastFiltered(ctx context.Context, excludeID string, commandID uint32, payload []byte) error {
	r.mu.RLock()
	targets := make([]Client, 0, len(r.clients))
	for id, c := range r.clients {
		if id != excludeID {
			targets = append(targets, c)
		}
	}
	r.mu.RUnlock()

	var failCount int
	for _, c := range targets {
		if err := c.Send(ctx, commandID, payload); err != nil {
			failCount++
		}
	}

	if failCount > 0 {
		return fmt.Errorf("room %s: failed to deliver to %d client(s)", r.id, failCount)
	}
	return nil
}

func (r *room) Close(ctx context.Context) {
	r.mu.Lock()
	clients := make([]Client, 0, len(r.clients))
	for _, c := range r.clients {
		clients = append(clients, c)
	}
	r.clients = make(map[string]Client)
	r.mu.Unlock()

	for _, c := range clients {
		c.Close(ctx) //nolint:errcheck
	}
}
