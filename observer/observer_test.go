package observer

import (
	"context"
	"testing"
	"time"

	"github.com/luciancaetano/knet/internal/room"
)

func TestObserverSetOwnerOnly(t *testing.T) {
	r := room.New("zone", 50*time.Millisecond)
	owner := &fakeClient{id: "owner"}
	other := &fakeClient{id: "other"}
	r.Add(owner)
	r.Add(other)

	set := NewSet(r, OwnerOnlyCondition(func(subject any) string {
		return subject.(string)
	}))

	observers := set.Observers("owner")
	if len(observers) != 1 || observers[0].ID() != "owner" {
		t.Fatalf("Observers() = %v, want only owner", observers)
	}

	if err := set.Broadcast(context.Background(), "owner", 1, []byte("hi")); err != nil {
		t.Fatalf("Broadcast() error = %v", err)
	}
	if len(owner.sent) != 1 {
		t.Fatal("expected owner to receive broadcast")
	}
	if len(other.sent) != 0 {
		t.Fatal("expected non-owner to not receive broadcast")
	}
}

func TestObserverSetDistance(t *testing.T) {
	r := room.New("zone", 50*time.Millisecond)
	near := &fakeClient{id: "near"}
	far := &fakeClient{id: "far"}
	r.Add(near)
	r.Add(far)

	positions := map[string][2]float64{
		"near": {0, 0},
		"far":  {100, 100},
	}
	subjectPos := [2]float64{0, 0}

	cond := DistanceCondition(
		func(clientID string) (float64, float64) { p := positions[clientID]; return p[0], p[1] },
		func(subject any) (float64, float64) { return subjectPos[0], subjectPos[1] },
		10,
	)
	set := NewSet(r, cond)

	observers := set.Observers(nil)
	if len(observers) != 1 || observers[0].ID() != "near" {
		t.Fatalf("Observers() = %v, want only near", observers)
	}
}

func TestObserverSetMultipleConditionsAND(t *testing.T) {
	r := room.New("zone", 50*time.Millisecond)
	c := &fakeClient{id: "a"}
	r.Add(c)

	set := NewSet(r, AlwaysCondition, NeverCondition)
	if observers := set.Observers(nil); len(observers) != 0 {
		t.Fatalf("Observers() = %v, want none (AND with NeverCondition)", observers)
	}
}

func TestObserverSetBroadcastPartialFailure(t *testing.T) {
	r := room.New("zone", 50*time.Millisecond)
	c1 := &fakeClient{id: "a", sendErr: errFakeSend}
	c2 := &fakeClient{id: "b"}
	r.Add(c1)
	r.Add(c2)

	set := NewSet(r, AlwaysCondition)
	err := set.Broadcast(context.Background(), nil, 1, []byte("hi"))
	if err == nil {
		t.Fatal("expected error when a client send fails")
	}
}
