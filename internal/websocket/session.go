package websocket

import (
	"sync"
	"time"

	"github.com/luciancaetano/knet"
)

// maxSessionEntries caps memorySessionStore size so a flood of involuntary
// disconnects (each minting a session pending resume) can't grow the map
// without bound until the process OOMs.
const maxSessionEntries = 100_000

// memorySessionStore is the default in-memory knet.SessionStore. Expiry is
// checked lazily on Get; Put also opportunistically sweeps expired entries
// and, if still over maxSessionEntries, evicts the soonest-to-expire ones.
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

	now := time.Now()
	for id, e := range s.entries {
		if now.After(e.expiresAt) {
			delete(s.entries, id)
		}
	}

	if len(s.entries) >= maxSessionEntries {
		s.evictSoonestExpiringLocked()
	}

	s.entries[sessionID] = sessionEntry{rooms: rooms, expiresAt: now.Add(ttl)}
}

// evictSoonestExpiringLocked removes the entry closest to expiry. Called with
// s.mu held, only once the store is at capacity.
func (s *memorySessionStore) evictSoonestExpiringLocked() {
	var oldestID string
	var oldestAt time.Time
	for id, e := range s.entries {
		if oldestID == "" || e.expiresAt.Before(oldestAt) {
			oldestID, oldestAt = id, e.expiresAt
		}
	}
	if oldestID != "" {
		delete(s.entries, oldestID)
	}
}

func (s *memorySessionStore) Delete(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.entries, sessionID)
}

var _ knet.SessionStore = (*memorySessionStore)(nil)
