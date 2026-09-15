package syncvar

import (
	"errors"
	"testing"
)

func encodeInt(v int) ([]byte, error) {
	return []byte{byte(v)}, nil
}

func TestSyncVarInitialNotDirty(t *testing.T) {
	sv := New(5, encodeInt)
	if sv.Get() != 5 {
		t.Fatalf("Get() = %d, want 5", sv.Get())
	}
	if sv.Dirty() {
		t.Fatal("expected new SyncVar not dirty")
	}
	if payload, changed := sv.Flush(); changed || payload != nil {
		t.Fatalf("Flush() on clean SyncVar = (%v, %v), want (nil, false)", payload, changed)
	}
}

func TestSyncVarSetMarksDirtyOnChange(t *testing.T) {
	sv := New(5, encodeInt)

	sv.Set(5) // same value, should not mark dirty
	if sv.Dirty() {
		t.Fatal("Set with same value should not mark dirty")
	}

	sv.Set(7)
	if !sv.Dirty() {
		t.Fatal("Set with new value should mark dirty")
	}

	payload, changed := sv.Flush()
	if !changed {
		t.Fatal("Flush() should report change")
	}
	if len(payload) != 1 || payload[0] != 7 {
		t.Fatalf("Flush() payload = %v, want [7]", payload)
	}
	if sv.Dirty() {
		t.Fatal("Flush() should clear dirty flag")
	}

	// second flush with no intervening Set: nothing to send
	if _, changed := sv.Flush(); changed {
		t.Fatal("second Flush() without change should report no change")
	}
}

func TestSyncVarFlushEncodeError(t *testing.T) {
	sv := New(1, func(int) ([]byte, error) { return nil, errors.New("boom") })
	sv.Set(2)
	payload, changed := sv.Flush()
	if changed || payload != nil {
		t.Fatalf("Flush() with encode error = (%v, %v), want (nil, false)", payload, changed)
	}
}
