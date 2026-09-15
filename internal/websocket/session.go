package websocket

import (
	"sync"
	"time"

	"github.com/luciancaetano/knet"
)

// memorySessionStore is the default in-memory knet.SessionStore. Expiry is
// checked lazily on Get — there is no background sweeper.
type memorySessionStore struct {
	mu      sync.Mutex
	entries map[string]sessionEntry
}

type sessionEntry struct {
	rooms     []string
	expiresAt time.Time
}

func newMemorySessionStore() *memorySessionStore {
	return &memorySessionStore{entries: make(map[string]sessionEntry)}
}

func (s *memorySessionStore) Get(sessionID string) ([]string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	e, ok := s.entries[sessionID]
	if !ok {
		return nil, false
	}
	if time.Now().After(e.expiresAt) {
		delete(s.entries, sessionID)
		return nil, false
	}
	return e.rooms, true
}

func (s *memorySessionStore) Put(sessionID string, rooms []string, ttl time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries[sessionID] = sessionEntry{rooms: rooms, expiresAt: time.Now().Add(ttl)}
}

func (s *memorySessionStore) Delete(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.entries, sessionID)
}

var _ knet.SessionStore = (*memorySessionStore)(nil)
