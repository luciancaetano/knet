package websocket

import (
	"testing"
	"time"
)

func TestMemorySessionStorePutGetDelete(t *testing.T) {
	t.Parallel()

	s := newMemorySessionStore()

	if _, ok := s.Get("missing"); ok {
		t.Fatal("Get on empty store should return ok=false")
	}

	s.Put("sess-1", []string{"room-a", "room-b"}, time.Minute)

	rooms, ok := s.Get("sess-1")
	if !ok {
		t.Fatal("Get after Put should return ok=true")
	}
	if len(rooms) != 2 || rooms[0] != "room-a" || rooms[1] != "room-b" {
		t.Errorf("got rooms %v, want [room-a room-b]", rooms)
	}

	s.Delete("sess-1")
	if _, ok := s.Get("sess-1"); ok {
		t.Fatal("Get after Delete should return ok=false")
	}
}

func TestMemorySessionStoreExpiry(t *testing.T) {
	t.Parallel()

	s := newMemorySessionStore()
	s.Put("sess-1", []string{"room-a"}, 20*time.Millisecond)

	if _, ok := s.Get("sess-1"); !ok {
		t.Fatal("Get before TTL expires should return ok=true")
	}

	time.Sleep(40 * time.Millisecond)

	if _, ok := s.Get("sess-1"); ok {
		t.Fatal("Get after TTL expires should return ok=false")
	}
}
