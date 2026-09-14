package syncvar

import (
	"errors"
	"testing"
)

func TestSyncVarInitialNotDirty(t *testing.T) {
	sv := NewInt32(5)
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
	sv := NewInt32(5)

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
	if len(payload) != 1+len("7") || payload[0] != byte(TagInt32) || string(payload[1:]) != "7" {
		t.Fatalf("Flush() payload = %v, want [tag=%02x, \"7\"]", payload, byte(TagInt32))
	}
	if sv.Dirty() {
		t.Fatal("Flush() should clear dirty flag")
	}

	// second flush with no intervening Set: nothing to send
	if _, changed := sv.Flush(); changed {
		t.Fatal("second Flush() without change should report no change")
	}
}

func TestDecodeRoundTripAllTypes(t *testing.T) {
	cases := []struct {
		name  string
		tag   Tag
		flush func() ([]byte, bool)
	}{
		{"int32", TagInt32, func() ([]byte, bool) { sv := NewInt32(41); sv.Set(42); return sv.Flush() }},
		{"int64", TagInt64, func() ([]byte, bool) { sv := NewInt64(0); sv.Set(1_000_000_000_000); return sv.Flush() }},
		{"float32", TagFloat32, func() ([]byte, bool) { sv := NewFloat32(0); sv.Set(3.5); return sv.Flush() }},
		{"float64", TagFloat64, func() ([]byte, bool) { sv := NewFloat64(0); sv.Set(2.7182818284); return sv.Flush() }},
		{"bool", TagBool, func() ([]byte, bool) { sv := NewBool(false); sv.Set(true); return sv.Flush() }},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			payload, changed := c.flush()
			if !changed {
				t.Fatal("Flush() should report a change")
			}
			got, err := Decode(payload, c.tag)
			if err != nil {
				t.Fatalf("Decode() error: %v", err)
			}
			if got == nil {
				t.Fatal("Decode() returned nil value")
			}
		})
	}
}

func TestDecodeString(t *testing.T) {
	sv := NewString("initial")
	sv.Set("hello world")
	payload, _ := sv.Flush()

	got, err := Decode(payload, TagString)
	if err != nil {
		t.Fatalf("Decode() error: %v", err)
	}
	if string(got.([]byte)) != "hello world" {
		t.Fatalf("Decode() = %q, want %q", got, "hello world")
	}
}

func TestDecodeTypeMismatch(t *testing.T) {
	sv := NewInt32(0)
	sv.Set(7)
	payload, _ := sv.Flush()

	if _, err := Decode(payload, TagFloat64); !errors.Is(err, ErrTypeMismatch) {
		t.Fatalf("Decode() wrong tag error = %v, want ErrTypeMismatch", err)
	}
}

func TestDecodeMalformedText(t *testing.T) {
	payload := []byte{byte(TagInt32), 'a', 'b', 'c'}
	if _, err := Decode(payload, TagInt32); err == nil {
		t.Fatal("Decode() malformed text should error")
	}
}
