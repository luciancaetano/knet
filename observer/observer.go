package observer

import (
	"context"
	"fmt"

	"github.com/luciancaetano/knet"
	"github.com/luciancaetano/knet/room"
)

// Condition decides, for one client, whether it should receive updates
// about subject.
//
// subject is opaque to knet — it can be a player ID, a game-object handle, a
// map-cell coordinate, or anything the application uses to identify what is
// being observed. This lets [Set] adapt to any interest-management scheme
// (grid, radius, scene, ownership, ...) without knet depending on any
// particular one.
type Condition interface {
	ShouldObserve(client knet.Client, subject any) bool
}

// ConditionFunc adapts a plain function to [Condition].
type ConditionFunc func(client knet.Client, subject any) bool

// ShouldObserve calls f.
func (f ConditionFunc) ShouldObserve(client knet.Client, subject any) bool {
	return f(client, subject)
}

// Set filters a [room.Room]'s clients through one or more [Condition]s
// before broadcasting, so updates about a subject only reach clients that
// currently have interest in it.
//
// All conditions must pass (logical AND). To express OR, wrap the conditions
// in a single custom [Condition].
//
// Observer/interest management is entirely opt-in: use [room.Room.Broadcast]
// directly when every client in a room should see every update.
type Set struct {
	room       room.Room
	conditions []Condition
}

// NewSet creates a Set scoped to rm, filtered by every given condition.
func NewSet(rm room.Room, conditions ...Condition) *Set {
	return &Set{room: rm, conditions: conditions}
}

// Observers returns the clients in the room that pass every condition for
// subject.
func (o *Set) Observers(subject any) []knet.Client {
	clients := o.room.Clients()
	out := make([]knet.Client, 0, len(clients))
	for _, c := range clients {
		if o.observes(c, subject) {
			out = append(out, c)
		}
	}
	return out
}

func (o *Set) observes(client knet.Client, subject any) bool {
	for _, cond := range o.conditions {
		if !cond.ShouldObserve(client, subject) {
			return false
		}
	}
	return true
}

// Broadcast sends commandID + payload to only the clients currently
// observing subject.
func (o *Set) Broadcast(ctx context.Context, subject any, commandID uint32, payload []byte) error {
	targets := o.Observers(subject)

	var failCount int
	for _, c := range targets {
		if err := c.Send(ctx, commandID, payload); err != nil {
			failCount++
		}
	}

	if failCount > 0 {
		return fmt.Errorf("observer set %s: failed to deliver to %d client(s)", o.room.ID(), failCount)
	}
	return nil
}
