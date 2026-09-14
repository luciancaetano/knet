# Rooms & Observer

Both packages are opt-in and layer on top of the base server — you don't need them for a simple echo server.

## Rooms

`room` package groups clients into named sets for scoped broadcasting: matches, lobbies, zones, chat channels. A client can belong to multiple rooms at once.

```go
import "github.com/luciancaetano/knet/room"

lobby := room.New("lobby-1")

config := ws.NewConfig(":8080", ws.DefaultRateLimitConfig(), ws.AllOrigins(),
	func(client knet.Client) bool { lobby.Add(client); return true },
	func(client knet.Client, voluntary bool) { lobby.Remove(client.ID()) },
)
server := ws.New(config)

server.RegisterHandler(ctx, ChatCmd, func(client knet.Client, payload []byte) {
	lobby.BroadcastExcept(ctx, client.ID(), ChatCmd, payload)
})
```

See the [Chat Example (JS)](chat-example.md) for a full, runnable walkthrough of `room` in a real multi-user chat server.

### API

| Method | Description |
|--------|-------------|
| `room.New(id string) room.Room` | create a room |
| `Add(client)` | insert a client (idempotent on reconnect) |
| `Remove(clientID)` | evict a client, no-op if absent |
| `Has(clientID) bool` | check membership |
| `Clients() []knet.Client` | snapshot of all clients in the room |
| `Broadcast(ctx, commandID, payload) error` | send to everyone in the room |
| `BroadcastExcept(ctx, excludeID, commandID, payload) error` | send to everyone but one client |
| `Close(ctx) error` | remove and disconnect all clients |

## Observer (interest management)

`observer` package filters a room's clients through one or more `Condition`s before broadcasting — so updates about a subject only reach clients that currently have interest in it (distance, ownership, scene, or any custom scheme).

```go
import "github.com/luciancaetano/knet/observer"

nearby := observer.ConditionFunc(func(client knet.Client, subject any) bool {
	playerID := subject.(string)
	return distance(client.ID(), playerID) < viewRadius
})

set := observer.NewSet(lobby, nearby)

// Send position updates only to clients close enough to see this player
set.Broadcast(ctx, playerID, PosCmd, positionPayload)
```

### API

| Type / Func | Description |
|---|---|
| `observer.Condition` | `ShouldObserve(client knet.Client, subject any) bool` — implement this or wrap a func in `ConditionFunc` |
| `observer.NewSet(room, conditions...) *Set` | scope a Set to one room, filtered by every given condition (logical AND) |
| `Set.Observers(subject any) []knet.Client` | clients in the room that pass every condition for `subject` |
| `Set.Broadcast(ctx, subject, commandID, payload) error` | send only to observers of `subject` |

`subject` is opaque to knet — a player ID, game-object handle, map-cell coordinate, or anything your app uses to identify what's being observed.

Use `room.Room.Broadcast` directly when every client in a room should see every update; reach for `observer.Set` only when interest varies per client.
