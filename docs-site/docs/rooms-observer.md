# Rooms & Observer

Both packages are opt-in and layer on top of the base server — you don't need them for a simple echo server.

## Rooms

A room groups clients into a named set for scoped broadcasting: matches, lobbies, zones, chat channels. A client can belong to multiple rooms at once.

Rooms can **only** be created through [`roommanager.Manager`](room-manager.md) — there is no public `room.New`/constructor to call directly. This is deliberate: a manually-created room would bypass the Manager's join/leave lifecycle and hooks (`OnAfterJoin`, `OnRoomClosed`, grace-period reconnect, etc.), which is a class of bug worth eliminating at the type-system level rather than by convention.

```go
import (
	"github.com/luciancaetano/knet"
	"github.com/luciancaetano/knet/roommanager"
)

hooks := &knet.ConnectHooks{}
rooms := roommanager.New(server, hooks, roommanager.Config{})

// Client joins/leaves via roommanager's own CmdRoomJoin/CmdRoomLeave commands
// (see Room Manager → Wire protocol) — no manual Add/Remove wiring needed.

server.RegisterHandler(ctx, ChatCmd, func(client knet.Client, payload []byte) {
	r, ok := rooms.Room("lobby-1")
	if !ok {
		return
	}
	r.BroadcastExcept(ctx, client.ID(), ChatCmd, payload)
})
```

See [Room Manager](room-manager.md) for the full setup (join/leave commands, hooks, reconnect grace periods, and the Colyseus-style `RoomHandler`/`Define` API) — that's what the [Chat Example](chat-example.md) uses.

### API

`Manager.Room(roomID)` returns a `room.View` — a read/broadcast-only facade, since membership (`Add`/`Remove`/`Close`) is owned by the Manager itself:

| Method | Description |
|--------|-------------|
| `ID() string` | the room's ID |
| `Has(clientID) bool` | check membership |
| `Clients() []knet.Client` | snapshot of all clients in the room |
| `Size() int` | current member count |
| `Broadcast(ctx, commandID, payload) error` | send to everyone in the room |
| `BroadcastExcept(ctx, excludeID, commandID, payload) error` | send to everyone but one client |

## Observer (interest management)

`observer` package filters a room's clients through one or more `Condition`s before broadcasting — so updates about a subject only reach clients that currently have interest in it (distance, ownership, scene, or any custom scheme).

```go
import "github.com/luciancaetano/knet/observer"

nearby := observer.ConditionFunc(func(client knet.Client, subject any) bool {
	playerID := subject.(string)
	return distance(client.ID(), playerID) < viewRadius
})

lobby, _ := rooms.Room("lobby-1")
set := observer.NewSet(lobby, nearby)

// Send position updates only to clients close enough to see this player
set.Broadcast(ctx, playerID, PosCmd, positionPayload)
```

### API

| Type / Func | Description |
|---|---|
| `observer.Condition` | `ShouldObserve(client knet.Client, subject any) bool` — implement this or wrap a func in `ConditionFunc` |
| `observer.NewSet(room room.View, conditions...) *Set` | scope a Set to one room (a `room.View`, e.g. from `Manager.Room`), filtered by every given condition (logical AND) |
| `Set.Observers(subject any) []knet.Client` | clients in the room that pass every condition for `subject` |
| `Set.Broadcast(ctx, subject, commandID, payload) error` | send only to observers of `subject` |

`subject` is opaque to knet — a player ID, game-object handle, map-cell coordinate, or anything your app uses to identify what's being observed.

Use the room's own `Broadcast` directly when every client in a room should see every update; reach for `observer.Set` only when interest varies per client.
