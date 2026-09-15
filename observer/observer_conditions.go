package observer

import "github.com/luciancaetano/knet"

// AlwaysCondition is a [Condition] that always observes. Useful as a
// default or for composing with other conditions in tests.
var AlwaysCondition Condition = ConditionFunc(func(knet.Client, any) bool { return true })

// NeverCondition is a [Condition] that never observes.
var NeverCondition Condition = ConditionFunc(func(knet.Client, any) bool { return false })

// OwnerOnlyCondition returns a [Condition] that only observes when the
// client is the owner of subject, as reported by ownerOf.
func OwnerOnlyCondition(ownerOf func(subject any) string) Condition {
	return ConditionFunc(func(client knet.Client, subject any) bool {
		return client.ID() == ownerOf(subject)
	})
}

// DistanceCondition returns a [Condition] that observes when the client is
// within radius of subject.
//
// clientPos and subjectPos are supplied by the caller, so this works with
// any coordinate system — world-space floats, grid cells converted to a
// center point, etc. knet has no notion of position itself.
func DistanceCondition(clientPos func(clientID string) (x, y float64), subjectPos func(subject any) (x, y float64), radius float64) Condition {
	radiusSq := radius * radius
	return ConditionFunc(func(client knet.Client, subject any) bool {
		cx, cy := clientPos(client.ID())
		sx, sy := subjectPos(subject)
		dx, dy := cx-sx, cy-sy
		return dx*dx+dy*dy <= radiusSq
	})
}
