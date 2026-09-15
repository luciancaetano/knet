package roommanager

import (
	"encoding/json"

	"github.com/luciancaetano/knet"
	"github.com/luciancaetano/knet/internal/room"
)

// RoomHandler is a per-room-instance lifecycle handler, one instance created
// per room by the Manager the first time a client joins with a matching
// RoomType (see [Manager.Define]). Analogous to Colyseus's Room class, scoped
// for now to OnCreate/OnJoin/OnLeave/OnDispose — auth/reconnect stay the job
// of [Manager.OnBeforeJoin] and [Manager.HandleResume].
type RoomHandler interface {
	// OnCreate is called once, when the room is created (first join).
	OnCreate(v room.View)
	// OnJoin is called every time a client joins this room instance.
	// metadata is the (possibly nil) RoomJoinRequest.Metadata the client sent
	// with the join — e.g. a display name — available before the join is
	// observable by any other member, so a handler can apply it (store a
	// name, validate it, reject the join) with no race against a separate
	// call.
	OnJoin(client knet.Client, metadata json.RawMessage)
	// OnLeave is called every time a client leaves (explicit leave,
	// voluntary disconnect, or grace-period expiry) this room instance.
	OnLeave(client knet.Client)
	// OnDispose is called once, when the room is closed (membership hits
	// zero). The handler instance is discarded after this call.
	OnDispose()
}

// RoomHandlerFactory creates a new [RoomHandler] instance for one room.
// Registered via [Manager.Define].
type RoomHandlerFactory func() RoomHandler
