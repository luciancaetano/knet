package timing

import (
	"context"
	"encoding/binary"
	"testing"
	"time"
)

func TestTimeManagerEnablePingBroadcastsOnInterval(t *testing.T) {
	srv := &fakeServer{}
	tm := New(srv, 10*time.Millisecond)

	if err := tm.EnablePing(context.Background(), srv, 2); err != nil {
		t.Fatalf("EnablePing() error = %v", err)
	}

	tm.dispatch(context.Background(), 0) // 0 % 2 == 0 -> ping
	tm.dispatch(context.Background(), 1) // 1 % 2 != 0 -> no ping
	tm.dispatch(context.Background(), 2) // ping again

	if len(srv.broadcasts) != 2 {
		t.Fatalf("expected 2 ping broadcasts, got %d", len(srv.broadcasts))
	}
	for _, b := range srv.broadcasts {
		if b.cmd != PingCommandID {
			t.Fatalf("broadcast cmd = %#x, want PingCommandID", b.cmd)
		}
	}
}

func TestTimeManagerEnablePingComputesRTT(t *testing.T) {
	srv := &fakeServer{}
	tm := New(srv, 50*time.Millisecond)

	if err := tm.EnablePing(context.Background(), srv, 1); err != nil {
		t.Fatalf("EnablePing() error = %v", err)
	}

	if _, ok := tm.RTT("p1"); ok {
		t.Fatal("expected no RTT before any pong")
	}

	// Simulate tick advancing to 5, then client echoing a ping sent at tick 3.
	tm.currentTick.Store(5)
	payload := make([]byte, 8)
	binary.BigEndian.PutUint64(payload, 3)

	handler, ok := srv.handlers[PingCommandID]
	if !ok {
		t.Fatal("expected PingCommandID handler to be registered")
	}
	client := &fakeClient{id: "p1"}
	handler(client, payload)

	rtt, ok := tm.RTT("p1")
	if !ok {
		t.Fatal("expected RTT after pong")
	}
	if want := 100 * time.Millisecond; rtt != want { // (5-3) ticks * 50ms
		t.Fatalf("RTT() = %v, want %v", rtt, want)
	}
}

func TestTimeManagerEnablePingIgnoresMalformedPayload(t *testing.T) {
	srv := &fakeServer{}
	tm := New(srv, 50*time.Millisecond)
	if err := tm.EnablePing(context.Background(), srv, 1); err != nil {
		t.Fatalf("EnablePing() error = %v", err)
	}

	handler := srv.handlers[PingCommandID]
	handler(&fakeClient{id: "p1"}, []byte("bad"))

	if _, ok := tm.RTT("p1"); ok {
		t.Fatal("expected no RTT recorded for malformed payload")
	}
}
