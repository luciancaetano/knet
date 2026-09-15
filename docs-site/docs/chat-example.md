# Chat Example (JS)

Full, step-by-step tutorial for building a multi-user chat with [`roommanager`](room-manager.md) (Go) on the server and a plain HTML/JavaScript client. The code in this tutorial is real and runs — it's in [`examples/chat/`](https://github.com/luciancaetano/knet/tree/main/examples/chat) in the repository.

## 1. Overview

We'll build:

- a Go server using `roommanager.Manager` to own a single room, `"lobby"`
- broadcasting of chat messages to everyone in the room, via `roommanager`'s built-in room-scoped message command
- automatic notifications of who joins and who leaves
- automatic reconnection on the client side, using the client-side `RoomManager` to rejoin the room
- the most important piece: distinguishing when someone **left on purpose** (clicked "Leave") from when they **dropped unexpectedly** (network went down, tab froze, timeout) — `roommanager` surfaces this natively via its grace-period membership

### Protocol

Almost everything here is `roommanager`'s own reserved protocol (`0xFFFE0001`-`0xFFFE0008` — full table in the [Room Manager reference](room-manager.md#wire-protocol)); the example adds exactly one custom command, for the one thing `roommanager` doesn't track — display names:

| ID | Direction | Payload | Purpose |
|---|---|---|---|
| `0x0001` SetName | client → server | text (desired name) | sets/updates the name on connect or reconnect |
| `0x0003` UserJoined | server → room | JSON `{"name"}` | someone joined, with their name |
| `0x0004` UserLeft | server → room | JSON `{"name"}` | someone left the room — explicit leave, or grace period expired — with their name |
| `CmdRoomMessage` | client → server → room | JSON `{"roomId","type":"chat","data"}` | chat message, `data` is the text |
| `CmdRoomMemberEvent` | server → room | `{"roomId","clientId","type"}` | built-in join/left/disconnected/reconnected, keyed by clientId |

## 2. Go server, step by step

The server is organized into 4 files, each with a single responsibility — not one monolithic `main.go`:

```
examples/chat/
├── protocol.go   # the one custom command ID and its payload type
├── names.go      # name storage keyed by clientID
├── server.go     # chatServer (shared state) + lobbyRoom (per-room presence logic)
└── main.go       # bootstrap: assembles the ws.Server, roommanager.Manager, and registers everything
```

### 2.1 `protocol.go` — Command IDs and payloads

We only need one custom command: `SetName`. Everything else — join, leave, presence, chat delivery — rides `roommanager`'s reserved commands. Application IDs must stay below `0xFFFFFFFC` — see [Reference](reference.md#reserved-command-ids).

```go
--8<-- "examples/chat/protocol.go:commands"
```

### 2.2 `names.go` — Storing each client's name

`knet.Client` doesn't store application state — only connection identity (`ID()`, `RemoteAddr()`, etc), and `roommanager` only knows `clientID`s, not display names. So the server keeps its own `clientID -> name` map, isolated in a dedicated type (`nameStore`) and protected by a mutex, since handlers run on concurrent goroutines.

```go
--8<-- "examples/chat/names.go:names"
```

### 2.3 `server.go` — the `chatServer` type

`chatServer` just holds shared state: the `roommanager.Manager` and the `nameStore`. `roomManager` starts out `nil` and gets set once, in `main()` (2.7) — it needs the `knet.Server` to register its handlers on, which doesn't exist yet at `newChatServer()`.

```go
--8<-- "examples/chat/server.go:server-type"
```

### 2.4 `SetName` handler

```go
--8<-- "examples/chat/server.go:setname-handler"
```

Just stores the name — the `UserJoined` presence broadcast happens from `lobbyRoom.OnJoin` (2.5), not here, since a client may `SetName` before or after joining the room. It's registered directly with `server.RegisterHandler(ctx, CmdSetName, chat.handleSetName)` — a real command handler, so it stays a method, not an inline closure.

Chat itself needs no server-side handler at all: `roommanager`'s built-in `CmdRoomMessage` command already forwards a `sendMessage` call to every other member of the room.

### 2.5 `lobbyRoom` — a Colyseus-style room class

Everything about presence for the `"lobby"` room type lives in one type, `lobbyRoom`, implementing `roommanager.RoomHandler`. If you've used [Colyseus](https://colyseus.io/), this is the same shape: `OnCreate`/`OnJoin`/`OnLeave`/`OnDispose`. `roommanager` creates one `lobbyRoom` instance per room instance, the first time a client joins with `roomType: "lobby"` (see [3.3](#33-wiring-it-all-up-in-the-ui)) — registered via `chat.roomManager.Define("lobby", ...)` in `main()` (2.7).

```go
--8<-- "examples/chat/server.go:lobby-room"
```

- `OnCreate` runs once, when the room instance is first created — it hands you `view room.View`, this room's own broadcast handle, which `lobbyRoom` keeps so `broadcastPresence` can call `r.view.Broadcast(...)` — scoped to just this room, never the whole server.
- `OnJoin`/`OnLeave` run for every member that joins or leaves this room instance (leave covers explicit leave *and* grace-period expiry — see [5](#5-voluntary-vs-involuntary-exit)).
- `OnDispose` runs once, when the room closes (membership hits zero) — nothing to clean up here.

### 2.6 Serving `index.html` — and why `wss://`

A browser that loads the page over `https://` is only allowed to open `wss://` sockets (not `ws://`) — it's the same "mixed content" rule that applies to `<img>`/`fetch`. Rather than teaching a setup that breaks the moment you deploy behind TLS in production, this example already runs with TLS from the start, using a self-signed development certificate.

`main.go` spins up a second `http.Server` (`serveStatic`), just to serve `index.html` and the mascot, on a port separate from the WebSocket port:

```go
--8<-- "examples/chat/main.go:static-server"
```

### 2.7 `main.go` — assembling the server

`main.go` assembles everything in a straight line, no jumping to another file to see what a callback does:

1. `newChatServer()`, then a `*knet.ConnectHooks` with two inline closures — connect logging, and disconnect logging + clearing the client's name — wired to `ws.Config` via `hooks.DispatchConnect`/`hooks.DispatchDisconnect` (`ws.WithTLS` enables `wss://`).
2. Once the `ws.Server` exists, `roommanager.New(server, hooks, roommanager.Config{})` — it self-registers connect/disconnect tracking on the same `hooks`.
3. `chat.roomManager.Define("lobby", func() roommanager.RoomHandler { return &lobbyRoom{names: chat.names} })` — registers the room type from 2.5. The factory runs once per room instance, not once per client.
4. `cfg.OnResume` — a single-slot field, not a `ConnectHooks` listener — wired to `roomManager.HandleResume` so a reconnect within the grace period resumes previous room membership.
5. The one remaining custom handler (`SetName`), the static file server, then `server.Start(ctx)` and graceful shutdown.

```go
--8<-- "examples/chat/main.go:bootstrap"
```

The certificate (`cert.pem`/`key.pem`) is generated automatically by `make chat-example` — see [Running the example](#6-running-the-example).

## 3. Web client, step by step

The client is a single `index.html`, with no build step. It uses the official [`@lcaetano/knet-client`](js-client.md) client and its [`RoomManager`](js-client.md#rooms), both loaded directly from a CDN via ESM — no reimplementing the wire format, reconnection, or room protocol by hand.

### 3.1 Importing the official client

```js
--8<-- "examples/chat/index.html:import"
```

`esm.sh` serves the package published on npm as a pure ES module, so all you need is a `<script type="module">` — no bundler, no `node_modules`.

### 3.2 Command IDs on the client

```js
--8<-- "examples/chat/index.html:commands"
```

Only `SetName` and the two presence commands are custom — `RoomManager` (constructed as `new RoomManager(client)`) owns join/leave/message internally.

### 3.3 Wiring it all up in the UI

```js
--8<-- "examples/chat/index.html:ui"
```

Important points:

- on `connected`, the client resends `SetName` **and** calls `rooms.joinRoom(ROOM_ID, ROOM_TYPE)` — both need to happen on every connect, initial or reconnect (see [4](#4-reconnection-in-practice)); `ROOM_TYPE` ("lobby") must match the key passed to `roomManager.Define` on the server (2.5), or the first join for a room has no `RoomHandler` factory to run
- `rooms.sendMessage(ROOM_ID, "chat", text)` replaces a raw `client.send(...)` call — it's scoped to the room, not broadcast to the whole server
- `rooms.on("message", ...)` delivers other members' chat messages, tagged with `senderId`; the client keeps a small local `clientId -> name` map (populated from `UserJoined`/`UserLeft`) to render a name instead of a raw ID
- `rooms.on("memberDisconnected", ...)` reports an involuntary drop for another member, sourced from `roommanager`'s grace-period tracking
- `client.disconnect()` turns off auto-reconnect and closes with a normal close frame, which makes the server see `voluntary = true`

## 4. Reconnection in practice

When the connection drops and reconnects automatically, the server assigns a **new `clientID`** to the handshake, and the client-side `RoomManager` clears `currentRooms` on every reconnect (see [Room Manager → reconnect caveat](room-manager.md#client-libraries)) — the client has no way yet to prove it's "the same user" as before. That's why the `connected` handler always resends `SetName` **and** re-joins `"lobby"`, both on the initial connection and on every reconnect: skip either and the server either has no name for the client, or the client stops receiving room messages entirely.

## 5. Voluntary vs. involuntary exit

This is the chat's trickiest requirement, and `roommanager` solves it for you via its grace-period membership:

| User action | What the server sees | Event emitted |
|---|---|---|
| Clicks "Leave" | Normal WebSocket close frame | `CmdRoomMemberEvent` type `left` (+ `UserLeft`) |
| Closes the browser tab | A close frame is usually sent by the browser too | `left` in most cases |
| Loses network connection (Wi-Fi drops, process frozen, cable unplugged) | No close frame arrives; the server detects it via read timeout | `CmdRoomMemberEvent` type `disconnected`, membership pending for `GraceTTL` |
| Client process is killed (`kill -9`, crash) | No close frame | `disconnected`, same grace period |
| Client reconnects within `GraceTTL` | `OnResume` fires with the previous room list | `CmdRoomMemberEvent` type `reconnected`, membership restored automatically |

There's no application code deciding this — it's the `voluntary` parameter of `OnDisconnect` (section [2.7](#27-maingo-assembling-the-server)) that carries this information, computed by the WebSocket protocol itself, and `roommanager.HandleDisconnect`/`HandleResume` that turn it into the right membership state machine. `lobbyRoom.OnLeave` (2.5) fires the same way whether the leave was explicit or a grace-period expiry — it doesn't need to know which.

## 6. Running the example

```bash
make chat-example
```

This first generates a self-signed development certificate (`examples/chat/cert.pem` + `key.pem`, via `openssl`, if it doesn't already exist — see the `chat-example-certs` target in the `Makefile`) and then starts the server:

- WebSocket: `wss://localhost:8080/ws`
- Chat page: `https://localhost:8081`

(Manual equivalent, without the Makefile: `cd examples/chat && go run .` — but the `.pem` files need to exist beforehand.)

Since the certificate is self-signed, the browser will warn about an "insecure connection" on the first visit to each of the two origins (`:8080` and `:8081`) — this is expected in development; accept the warning on both. In production, prefer terminating TLS at a reverse proxy (nginx, Caddy) with a real certificate, as described in `ws.WithTLS`.

Open `https://localhost:8081` in two tabs:

1. In each tab, type a different name and click **Connect**
2. Exchange messages — they appear in both tabs
3. In one tab, click **Leave** → the other tab shows `"<name> left"`
4. In the other tab, just close the browser tab or turn off the network → after the read timeout, you'll see `"<name> dropped (connection lost)"` (this can take a few seconds, depending on the `readDeadline` configured on the server)

## 7. Next steps

- [Room Manager](room-manager.md) — full API reference: hooks, config, wire protocol
- [Rooms & Observer](rooms-observer.md) — the lower-level `room` primitive `roommanager` builds on, plus interest management
- [JavaScript Client](js-client.md) — full reference for `KNetClient`: JSON-RPC, sync vars, configurable reconnection, and more
- [Server API](server-api.md) — full server configuration, rate limiting, security
