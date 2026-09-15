package knet

import "time"

// SessionStore persists which rooms a client session was in, keyed by
// session ID, to support reconnect/resume. It stores only room ID lists —
// never game state or message history.
//
// The default implementation is in-memory; supply your own (Redis, etc.) to
// share sessions across multiple server instances.
type SessionStore interface {
	// Get returns the room IDs associated with sessionID, if present and not
	// expired.
	Get(sessionID string) (rooms []string, ok bool)

	// Put stores the room IDs for sessionID, expiring after ttl.
	Put(sessionID string, rooms []string, ttl time.Duration)

	// Delete removes sessionID from the store.
	Delete(sessionID string)
}
