package syncvar

import (
	"errors"
	"fmt"
	"strconv"
	"sync"
)

// Tag identifies a SyncVar wire type. It is the first byte of every Flush payload.
type Tag uint8

// SyncVar wire types. Both client encoders (Knet.Unity SyncVarAttribute,
// @knet/client syncvar helpers) match these values: encode a value as its
// UTF-8 text after this 1-byte tag.
const (
	TagInt32   Tag = 0x01 // int32, text via strconv.Itoa
	TagInt64   Tag = 0x02 // int64, text via strconv.FormatInt
	TagFloat32 Tag = 0x03 // float32, text via strconv.FormatFloat 'f' -1
	TagFloat64 Tag = 0x04 // float64, text via strconv.FormatFloat 'f' -1
	TagBool    Tag = 0x05 // bool, text via strconv.FormatBool
	TagString  Tag = 0x06 // string, raw UTF-8
)

// ErrTypeMismatch is returned by Decode when a payload's tag does not match
// the expected tag for that field.
var ErrTypeMismatch = errors.New("syncvar: payload tag does not match sync var type")

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
// reflection. Only the primitive types below are wired for the wire format
// (NewInt32, NewInt64, NewFloat32, NewFloat64, NewBool, NewString).
//
// Wire format of a Flush payload (same contract on every client):
//
//	[0]     tag — SyncVar wire type (TagInt32 … TagString / Knet SyncVarType)
//	[1..]   value — UTF-8 text
//	        int32/int64/bool → decimal digits ("42", "true")
//	        float32/float64  → strconv.FormatFloat 'f' -1 ("3.14")
//	        string           → raw bytes, no escaping
//
// Example:
//
//	posX := syncvar.NewFloat32(0, func(v float32) ([]byte, error) {
//	    return []byte(strconv.FormatFloat(float64(v), 'f', -1, 32)), nil
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
//
// SyncVar is safe for concurrent use.
type SyncVar[T comparable] struct {
	tag    Tag
	value  T
	dirty  bool
	encode func(T) ([]byte, error)
	mu     sync.RWMutex
}

// NewInt32 creates a SyncVar encoding as TagInt32.
func NewInt32(initial int32) *SyncVar[int32] {
	return &SyncVar[int32]{tag: TagInt32, value: initial, encode: encodeInt32}
}

// NewInt64 creates a SyncVar encoding as TagInt64.
func NewInt64(initial int64) *SyncVar[int64] {
	return &SyncVar[int64]{tag: TagInt64, value: initial, encode: encodeInt64}
}

// NewFloat32 creates a SyncVar encoding as TagFloat32.
func NewFloat32(initial float32) *SyncVar[float32] {
	return &SyncVar[float32]{tag: TagFloat32, value: initial, encode: encodeFloat32}
}

// NewFloat64 creates a SyncVar encoding as TagFloat64.
func NewFloat64(initial float64) *SyncVar[float64] {
	return &SyncVar[float64]{tag: TagFloat64, value: initial, encode: encodeFloat64}
}

// NewBool creates a SyncVar encoding as TagBool.
func NewBool(initial bool) *SyncVar[bool] {
	return &SyncVar[bool]{tag: TagBool, value: initial, encode: encodeBool}
}

// NewString creates a SyncVar encoding as TagString.
func NewString(initial string) *SyncVar[string] {
	return &SyncVar[string]{tag: TagString, value: initial, encode: encodeString}
}

// Tag returns the wire type this SyncVar encodes as.
func (s *SyncVar[T]) Tag() Tag { return s.tag }

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

// Flush encodes the current value (prefixed with its wire type tag) and clears
// the dirty flag, but only if the value changed since the last Flush.
// Returns (nil, false) when there is nothing new to send.
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
	out := make([]byte, 1+len(payload))
	out[0] = byte(s.tag)
	copy(out[1:], payload)
	return out, true
}

// Decode parses a Flush payload into a value of the given wire type.
// It returns ErrTypeMismatch when the payload's leading tag does not belong
// to type (useful for validating that an incoming message matches a declared
// SyncVar type), and a *strconv.NumError for malformed text.
func Decode(data []byte, typ Tag) (any, error) {
	if len(data) < 1 {
		return nil, errors.New("syncvar: empty sync var payload")
	}
	if Tag(data[0]) != typ {
		return nil, ErrTypeMismatch
	}
	text := string(data[1:])
	switch typ {
	case TagInt32:
		v, err := strconv.ParseInt(text, 10, 32)
		return int32(v), err
	case TagInt64:
		v, err := strconv.ParseInt(text, 10, 64)
		return v, err
	case TagFloat32:
		v, err := strconv.ParseFloat(text, 32)
		return float32(v), err
	case TagFloat64:
		v, err := strconv.ParseFloat(text, 64)
		return v, err
	case TagBool:
		v, err := strconv.ParseBool(text)
		return v, err
	case TagString:
		return data[1:], nil
	default:
		return nil, fmt.Errorf("syncvar: unknown tag %#02x", byte(typ))
	}
}

// The six wire encoders. Text formats match the clients: strconv for numbers
// and bool (so Parse* round-trips exactly), raw bytes for string.

func encodeInt32(v int32) ([]byte, error) { return []byte(strconv.FormatInt(int64(v), 10)), nil }
func encodeInt64(v int64) ([]byte, error) { return []byte(strconv.FormatInt(v, 10)), nil }
func encodeFloat32(v float32) ([]byte, error) {
	return []byte(strconv.FormatFloat(float64(v), 'f', -1, 32)), nil
}
func encodeFloat64(v float64) ([]byte, error) {
	return []byte(strconv.FormatFloat(v, 'f', -1, 64)), nil
}
func encodeBool(v bool) ([]byte, error)     { return []byte(strconv.FormatBool(v)), nil }
func encodeString(v string) ([]byte, error) { return []byte(v), nil }
