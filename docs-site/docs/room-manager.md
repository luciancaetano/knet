# Room Manager

`roommanager` is an optional, opt-in layer on top of [`room.Room`](rooms-observer.md#rooms). It adds a named/discoverable multi-room registry, reserved join/leave/message commands, automatic create-on-first-join / close-on-empty room lifecycle, and grace-period disconnect/reconnect handling for room membership — the things you'd otherwise hand-roll on top of `room` (see the [Chat Example](chat-example.md), which used to do exactly that).

## Setup

`roommanager.New` never modifies knet core — it's constructed explicitly by the application and wired into `ServerConfig`'s `OnConnect`/`OnClientDisconnect`/`OnResume` hooks by hand, since those hooks hold a single function reference each:

```go
import "github.com/luciancaetano/knet/roommanager"

cfg := ws.NewConfig(":8080", ws.DefaultRateLimitConfig(), ws.AllOrigins(),
	onConnect, onDisconnect)
server := ws.New(cfg)

rooms := roommanager.New(server, roommanager.Config{})

func onConnect(client knet.Client) bool {
	return rooms.HandleConnect(client)
}

func onDisconnect(client knet.Client, voluntary bool) {
	rooms.HandleDisconnect(client, voluntary)
}

// Optional but recommended: resume pending room membership after a reconnect.
cfg.OnResume = func(client knet.Client, previousRooms []string) bool {
	return rooms.HandleResume(client, previousRooms)
}
```

An application that never calls `roommanager.New` has zero behavior change — the package touches nothing until you wire it in.

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

| Method | Description |
|---|---|
| `Room(roomID) (room.Room, bool)` | the underlying `room.Room`, for direct `Broadcast`/`Clients()` access |
| `RoomsOf(clientID) []string` | rooms a client currently belongs to |
| `Rooms() []string` | all currently open room IDs |

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

`CmdRoomMessage` is handled off the connection's read loop (bounded worker pool) and fans out concurrently per target — sending to a room does not block the sender's connection, and a client may be a member of several rooms at once without the message handling serializing across them.

## Client libraries

Both first-party clients wrap this protocol — see their "Rooms" sections for usage: [JS client](js-client.md#rooms), [Unity client](unity-client.md#rooms).

**Reconnect caveat:** neither client currently persists/resends a session ID across reconnects, so the server can never treat a reconnect as a resume from the client's side today — `HandleResume`/`CmdRoomResumeSync` are wired and functional, but only reachable once a client sends back the session ID it was assigned. Until then, every client reconnect clears its local room list and fires a `roomsLost`/`OnRoomsLost` event instead of auto-resuming.
