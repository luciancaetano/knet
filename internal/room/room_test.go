package room

import (
	"context"
	"testing"
	"time"
)

func TestRoomAddHasRemoveSize(t *testing.T) {
	r := New("lobby", 50*time.Millisecond)
	if r.ID() != "lobby" {
		t.Fatalf("ID() = %q, want lobby", r.ID())
	}
	if r.Size() != 0 {
		t.Fatalf("Size() = %d, want 0", r.Size())
	}

	c1 := &fakeClient{id: "a"}
	c2 := &fakeClient{id: "b"}
	r.Add(c1)
	r.Add(c2)

	if !r.Has("a") || !r.Has("b") {
		t.Fatal("expected both clients present")
	}
	if r.Size() != 2 {
		t.Fatalf("Size() = %d, want 2", r.Size())
	}

	// Re-add with same ID replaces, doesn't duplicate.
	r.Add(&fakeClient{id: "a"})
	if r.Size() != 2 {
		t.Fatalf("Size() after re-add = %d, want 2", r.Size())
	}

	r.Remove("a")
	if r.Has("a") {
		t.Fatal("expected a removed")
	}
	if r.Size() != 1 {
		t.Fatalf("Size() after remove = %d, want 1", r.Size())
	}

	r.Remove("nonexistent") // no-op, shouldn't panic
}

func TestRoomClients(t *testing.T) {
	r := New("lobby", 50*time.Millisecond)
	r.Add(&fakeClient{id: "a"})
	r.Add(&fakeClient{id: "b"})

	clients := r.Clients()
	if len(clients) != 2 {
		t.Fatalf("Clients() len = %d, want 2", len(clients))
	}
}

func TestRoomBroadcast(t *testing.T) {
	r := New("lobby", 50*time.Millisecond)
	c1 := &fakeClient{id: "a"}
	c2 := &fakeClient{id: "b"}
	r.Add(c1)
	r.Add(c2)

	if err := r.Broadcast(context.Background(), 1, []byte("hi")); err != nil {
		t.Fatalf("Broadcast() error = %v", err)
	}
	if len(c1.sent) != 1 || len(c2.sent) != 1 {
		t.Fatal("expected both clients to receive broadcast")
	}
}

func TestRoomBroadcastExcept(t *testing.T) {
	r := New("lobby", 50*time.Millisecond)
	c1 := &fakeClient{id: "a"}
	c2 := &fakeClient{id: "b"}
	r.Add(c1)
	r.Add(c2)

	if err := r.BroadcastExcept(context.Background(), "a", 1, []byte("hi")); err != nil {
		t.Fatalf("BroadcastExcept() error = %v", err)
	}
	if len(c1.sent) != 0 {
		t.Fatal("excluded client should not receive broadcast")
	}
	if len(c2.sent) != 1 {
		t.Fatal("expected c2 to receive broadcast")
	}
}

func TestRoomBroadcastPartialFailure(t *testing.T) {
	r := New("lobby", 50*time.Millisecond)
	c1 := &fakeClient{id: "a", sendErr: errFakeSend}
	c2 := &fakeClient{id: "b"}
	r.Add(c1)
	r.Add(c2)

	err := r.Broadcast(context.Background(), 1, []byte("hi"))
	if err == nil {
		t.Fatal("expected error when a client send fails")
	}
}

func TestRoomClose(t *testing.T) {
	r := New("lobby", 50*time.Millisecond)
	c1 := &fakeClient{id: "a"}
	c2 := &fakeClient{id: "b"}
	r.Add(c1)
	r.Add(c2)

	r.Close(context.Background())

	if !c1.closed || !c2.closed {
		t.Fatal("expected all clients closed")
	}
	if r.Size() != 0 {
		t.Fatalf("Size() after Close = %d, want 0", r.Size())
	}
}
