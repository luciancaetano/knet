package main

import "sync"

// --8<-- [start:names]
// nameStore maps clientID -> display name. The knet.Client interface has no
// slot for application state, so the server keeps this mapping itself,
// guarded by a mutex since handlers run concurrently on their own goroutines.
type nameStore struct {
	mu sync.RWMutex
	m  map[string]string
}

func newNameStore() *nameStore {
	return &nameStore{m: make(map[string]string)}
}

func (n *nameStore) set(clientID, name string) {
	n.mu.Lock()
	n.m[clientID] = name
	n.mu.Unlock()
}

// get returns the stored name, or clientID itself if no SetName was ever
// received (defensive fallback — should not normally happen).
func (n *nameStore) get(clientID string) string {
	n.mu.RLock()
	defer n.mu.RUnlock()
	if v, ok := n.m[clientID]; ok {
		return v
	}
	return clientID
}

func (n *nameStore) delete(clientID string) {
	n.mu.Lock()
	delete(n.m, clientID)
	n.mu.Unlock()
}

// --8<-- [end:names]
