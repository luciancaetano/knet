package syncvar

import "sync"

// SyncVar holds a piece of server-authoritative state that should be
// replicated to clients only when it changes.
//
// Call [SyncVar.Set] whenever the value changes in application code, then
// call [SyncVar.Flush] from a [TimeManager] handler (or any other broadcast loop)
// to get the encoded payload and clear the dirty flag. Flush returns
// (nil, false) when nothing changed since the last flush, letting callers
// skip the broadcast entirely — avoiding wasted bandwidth for unchanged
// state.
//
// T must be comparable: Set uses == to detect changes without paying for
// reflection. Types that are not comparable (slices, maps, funcs) don't
// satisfy the constraint — wrap them in a comparable struct (e.g. a version
// counter) or manage dirty-tracking manually for those cases.
//
// SyncVar is safe for concurrent use.
//
// Example:
//
//	posX := syncvar.New(0.0, func(v float64) ([]byte, error) {
//	    return []byte(strconv.FormatFloat(v, 'f', -1, 64)), nil
//	})
//
//	// Application code, whenever the value changes:
//	posX.Set(newX)
//
//	// TimeManager handler, every tick:
//	tm.RegisterRoom(room, PosCmd, func(tick uint64) []byte {
//	    payload, changed := posX.Flush()
//	    if !changed {
//	        return nil
//	    }
//	    return payload
//	})
type SyncVar[T comparable] struct {
	mu     sync.RWMutex
	value  T
	dirty  bool
	encode func(T) ([]byte, error)
}

// New creates a SyncVar with the given initial value.
//
// encode is called by [SyncVar.Flush] to produce the wire payload; it is
// required and must not be nil.
func New[T comparable](initial T, encode func(T) ([]byte, error)) *SyncVar[T] {
	return &SyncVar[T]{value: initial, encode: encode}
}

// Get returns the current value.
func (s *SyncVar[T]) Get() T {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.value
}

// Set updates the value. If v differs from the current value, the SyncVar is
// marked dirty so the next [SyncVar.Flush] produces a payload.
func (s *SyncVar[T]) Set(v T) {
	s.mu.Lock()
	if v != s.value {
		s.value = v
		s.dirty = true
	}
	s.mu.Unlock()
}

// Dirty reports whether the value has changed since the last [SyncVar.Flush].
func (s *SyncVar[T]) Dirty() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.dirty
}

// Flush encodes the current value and clears the dirty flag, but only if the
// value changed since the last Flush. Returns (nil, false) when there is
// nothing new to send.
func (s *SyncVar[T]) Flush() ([]byte, bool) {
	s.mu.Lock()
	if !s.dirty {
		s.mu.Unlock()
		return nil, false
	}
	v := s.value
	s.dirty = false
	s.mu.Unlock()

	payload, err := s.encode(v)
	if err != nil {
		return nil, false
	}
	return payload, true
}
