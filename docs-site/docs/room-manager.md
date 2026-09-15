# Room Manager

`roommanager` is an optional, opt-in layer on top of knet's internal room primitive. Rooms are **never created directly by application code** — only ever by a `Manager` (create-on-first-join / close-on-empty) — so every room's lifecycle always goes through this package's hooks. It adds a named/discoverable multi-room registry, reserved join/leave/message commands, and grace-period disconnect/reconnect handling for room membership — the things you'd otherwise hand-roll on top of a bare room type (see the [Chat Example](chat-example.md), which used to do exactly that).

## Setup

`roommanager.New` never modifies knet core. It takes the same [`*knet.ConnectHooks`](#connecthooks) you wired into `ws.Config` and registers its own connect/disconnect tracking on it — so an application can't forget to wire that up, and can still add its own `OnConnect`/`OnDisconnect` listeners on the same hooks alongside it. `hooks` is **mandatory**: passing `nil` panics.

```go
import (
	"github.com/luciancaetano/knet"
	"github.com/luciancaetano/knet/roommanager"
)

hooks := &knet.ConnectHooks{}
hooks.OnConnect(func(c knet.Client) bool {
	log.Printf("connected: %s", c.ID())
	return true
})
hooks.OnDisconnect(func(c knet.Client, voluntary bool) {
	log.Printf("disconnected: %s voluntary=%v", c.ID(), voluntary)
})

cfg := ws.NewConfig(":8080", ws.DefaultRateLimitConfig(), ws.AllOrigins(),
	hooks.DispatchConnect, hooks.DispatchDisconnect)
server := ws.New(cfg)

rooms := roommanager.New(server, hooks, roommanager.Config{})

// Optional but recommended: resume pending room membership after a reconnect.
// OnResume has no multi-listener fan-out (single ServerConfig field), so it
// still needs manual wiring.
cfg.OnResume = func(client knet.Client, previousRooms []string) bool {
	return rooms.HandleResume(client, previousRooms)
}
```

An application that never calls `roommanager.New` has zero behavior change — the package touches nothing until you wire it in.

### ConnectHooks

`ws.Config`'s `onConnect`/`onDisconnect` fields hold a single function reference each. `knet.ConnectHooks` fans that single slot out to multiple independent listeners — e.g. your own app logic and `roommanager`'s tracking, registered independently without knowing about each other:

```go
hooks := &knet.ConnectHooks{}
hooks.OnConnect(appLogic.onConnect)   // your listener
hooks.OnDisconnect(appLogic.onDisconnect)
// roommanager.New adds its own listeners on the same hooks internally.
```

`DispatchConnect` runs every registered `OnConnect` listener in registration order and returns `false` (rejecting the connection) if any of them does; `DispatchDisconnect` runs every `OnDisconnect` listener unconditionally.

## Config

| Field | Default | Purpose |
|---|---|---|
| `GraceTTL` | 30s (matches knet's own `SessionGraceTTL`) | how long a room membership stays pending after an involuntary disconnect, giving the client a window to reconnect and resume |
| `MaxMembersPerRoom` | 0 (unlimited) | caps how many clients may join a single room |

## Hooks

| Hook | Fires | Can reject? |
|---|---|---|
| `OnBeforeJoin(func(client, roomID) error)` | before a join is accepted | yes — returned error becomes the join's error response |
| `OnAfterJoin(func(client, roomID))` | after a join succeeds | no |
| `OnBeforeLeave(func(client, roomID))` | before a leave/removal | no |
| `OnAfterLeave(func(client, roomID))` | after a leave/removal | no |
| `OnRoomCreated(func(roomID))` | a room is created on first join | no |
| `OnRoomClosed(func(roomID))` | a room closes on last leave | no |
| `OnRoomMessage(func(client, roomID, msgType, data) error)` | before a `sendMessage` is forwarded | yes — returned error rejects the message |

Only one handler per hook; calling the setter again replaces the previous one.

## Reading room state

Rooms can only ever be created by a `Manager` — the constructor and the mutable room interface are not exported outside the library, so there's no way for application code to create a room that bypasses the Manager's join/leave lifecycle and its hooks. What you get back is a **facade**, `room.View` — read/broadcast only:

| Method | Description |
|---|---|
| `Room(roomID) (room.View, bool)` | read/broadcast-only view of the room: `ID()`, `Has()`, `Clients()`, `Size()`, `Broadcast()`, `BroadcastExcept()`, `Clock()` |
| `RoomsOf(clientID) []string` | rooms a client currently belongs to |
| `Rooms() []string` | all currently open room IDs |
| `Server() knet.Server` | the underlying server this Manager is attached to |
| `GetClient(clientID) (knet.Client, bool)` | the connected client, if it's a member of any tracked room |
| `GetClients() []knet.Client` | every distinct client currently a member of a tracked room |

## RoomHandler — per-room-instance lifecycle (Colyseus-style)

For rooms that need their own state (a match, a game instance) instead of routing everything through the flat global hooks above, register a `RoomHandler` factory under a room type name with `Define`. The Manager instantiates one handler per room, the first time a client joins with a matching `RoomType`, and drives its lifecycle automatically:

```go
type RoomHandler interface {
	OnCreate(v room.View)      // once, when the room is created (first join)
	OnJoin(client knet.Client) // every join to this room instance
	OnLeave(client knet.Client) // every leave (explicit, disconnect, or grace expiry)
	OnDispose()                // once, when the room closes (membership hits zero)
}
```

```go
type MatchRoom struct {
	view    room.View
	players map[string]bool
}

func (r *MatchRoom) OnCreate(v room.View)   { r.view = v; r.players = map[string]bool{} }
func (r *MatchRoom) OnJoin(c knet.Client)   { r.players[c.ID()] = true }
func (r *MatchRoom) OnLeave(c knet.Client)  { delete(r.players, c.ID()) }
func (r *MatchRoom) OnDispose()             { /* cleanup */ }

rooms.Define("match", func() roommanager.RoomHandler { return &MatchRoom{} })
```

The client requests a typed room by sending `RoomType` alongside `RoomID` on `CmdRoomJoin` (see [Wire protocol](#wire-protocol) below). `Define` is purely additive: the global hooks (`OnAfterJoin`, etc.) keep firing for every room regardless of type, so existing flat-mode apps (e.g. the [Chat Example](chat-example.md), which never sets `RoomType`) are unaffected. Scope is intentionally limited to `OnCreate`/`OnJoin`/`OnLeave`/`OnDispose` — auth-like rejection and reconnects are still `OnBeforeJoin` and `HandleResume`'s job.

## Wire protocol

Reserved command range `0xFFFE0001`–`0xFFFE0008` — do not register handlers on these yourself.

| ID | Constant | Direction | Purpose |
|---|---|---|---|
| `0xFFFE0001` | `CmdRoomJoin` | client → server | request to join a room |
| `0xFFFE0002` | `CmdRoomLeave` | client → server | request to leave a room |
| `0xFFFE0003` | `CmdRoomJoinAck` | server → client | join acknowledgement, with member list |
| `0xFFFE0004` | `CmdRoomLeaveAck` | server → client | leave acknowledgement |
| `0xFFFE0005` | `CmdRoomError` | server → client | room operation error |
| `0xFFFE0006` | `CmdRoomMemberEvent` | server → client | a member joined/left/disconnected/reconnected |
| `0xFFFE0007` | `CmdRoomResumeSync` | server → client | resumed session's room membership sync |
| `0xFFFE0008` | `CmdRoomMessage` | both | client → server to send, server → client to deliver, an arbitrary room-scoped message |

`RoomJoinRequest` (payload of `CmdRoomJoin`) carries an optional `roomType` field naming a handler registered via `Define` (see above). Omit it (or send `""`) for a legacy flat room with no handler instance — fully backward compatible.

`CmdRoomMessage` is handled off the connection's read loop (bounded worker pool) and fans out concurrently per target — sending to a room does not block the sender's connection, and a client may be a member of several rooms at once without the message handling serializing across them.

## Client libraries

Both first-party clients wrap this protocol — see their "Rooms" sections for usage: [JS client](js-client.md#rooms), [Unity client](unity-client.md#rooms).

**Reconnect caveat:** neither client currently persists/resends a session ID across reconnects, so the server can never treat a reconnect as a resume from the client's side today — `HandleResume`/`CmdRoomResumeSync` are wired and functional, but only reachable once a client sends back the session ID it was assigned. Until then, every client reconnect clears its local room list and fires a `roomsLost`/`OnRoomsLost` event instead of auto-resuming.
