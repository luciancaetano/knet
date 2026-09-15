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

Almost everything here is `roommanager`'s own reserved protocol (`0xFFFE0001`-`0xFFFE0008` — full table in the [Room Manager reference](room-manager.md#wire-protocol)); the example adds exactly two custom commands, for the one thing `roommanager` doesn't track itself — display names:

| ID | Direction | Payload | Purpose |
|---|---|---|---|
| `CmdRoomJoin` (`roommanager`) | client → server | JSON `{"roomId","roomType","metadata":{"name"}}` | join the room — the display name rides along as join metadata, not a separate command |
| `0x0003` UserJoined | server → room | JSON `{"clientId","name"}` | someone joined, with their clientID and name |
| `0x0004` UserLeft | server → room | JSON `{"clientId","name"}` | someone left the room — explicit leave, or grace period expired — with their clientID and name |
| `0x0005` RoomSnapshot | server → joining client only | JSON `{"members":[{"clientId","name"}]}` | names of members already in the room, sent once right after you join — `UserJoined` only fires for members who join *after* you |
| `CmdRoomMessage` | client → server → room | JSON `{"roomId","type":"chat","data"}` | chat message, `data` is the text |
| `CmdRoomMemberEvent` | server → room | `{"roomId","clientId","type"}` | built-in join/left/disconnected/reconnected, keyed by clientId |

## 2. Go server, step by step

The server is organized into 4 files, each with a single responsibility — not one monolithic `main.go`:

```
examples/chat/
├── protocol.go   # custom command IDs and payload types
├── names.go      # name storage keyed by clientID
├── server.go     # chatServer (shared state) + lobbyRoom (per-room presence logic)
└── main.go       # bootstrap: assembles the ws.Server, roommanager.Manager, and registers everything
```

### 2.1 `protocol.go` — Command IDs and payloads

We only need two custom commands, `UserJoined`/`UserLeft` — everything else, including the display name itself, rides `roommanager`'s reserved commands (the name travels as `CmdRoomJoin`'s `metadata`, see [2.4](#24-lobbyroom--a-colyseus-style-room-class)). Application IDs must stay below `0xFFFFFFFC` — see [Reference](reference.md#reserved-command-ids).

```go
--8<-- "examples/chat/protocol.go:commands"
```

### 2.2 `names.go` — Storing each client's name

`knet.Client` doesn't store application state — only connection identity (`ID()`, `RemoteAddr()`, etc), and `roommanager` only knows `clientID`s, not display names. So the server keeps its own `clientID -> name` map, isolated in a dedicated type (`nameStore`) and protected by a mutex, since handlers run on concurrent goroutines.

```go
--8<-- "examples/chat/names.go:names"
```

### 2.3 `server.go` — the `chatServer` type

`chatServer` just holds shared state: the `roommanager.Manager` and the `nameStore`. `roomManager` starts out `nil` and gets set once, in `main()` (2.6) — it needs the `knet.Server` to register its handlers on, which doesn't exist yet at `newChatServer()`.

```go
--8<-- "examples/chat/server.go:server-type"
```

### 2.4 `lobbyRoom` — a Colyseus-style room class

Everything about presence for the `"lobby"` room type lives in one type, `lobbyRoom`, implementing `roommanager.RoomHandler`. If you've used [Colyseus](https://colyseus.io/), this is the same shape: `OnCreate`/`OnJoin`/`OnLeave`/`OnDispose`. `roommanager` creates one `lobbyRoom` instance per room instance, the first time a client joins with `roomType: "lobby"` (see [3.3](#33-wiring-it-all-up-in-the-ui)) — registered via `chat.roomManager.Define("lobby", ...)` in `main()` (2.6).

```go
--8<-- "examples/chat/server.go:lobby-room"
```

- `OnCreate` runs once, when the room instance is first created — it hands you `view room.View`, this room's own broadcast handle, which `lobbyRoom` keeps so `broadcastPresence` can call `r.view.Broadcast(...)` — scoped to just this room, never the whole server.
- `OnJoin` runs for every member that joins this room instance. `metadata` is the join's own payload — `{"name": "..."}` here (see `joinMetadata` in `protocol.go`) — sent by the client as part of `joinRoom`'s call itself (3.3), not a separate command. That's deliberate: the server dispatches command handlers concurrently from a worker pool, so a name set via any *other* command would have no guaranteed order relative to the join — `OnJoin` could run first and broadcast an empty name. Because the name arrives embedded in the join request, `OnJoin` always has it before it does anything else, with no race to reason about.
- `OnJoin` also calls `sendSnapshot`, sending the new client alone (`client.Send`, not `view.Broadcast`) the names of everyone already in the room (`CmdRoomSnapshot`). This isn't a race fix — it's a real gap `UserJoined` alone can't cover: that event only ever fires for members who join *after* you, so a client joining a room with existing members would otherwise never learn their names, permanently, not just during a narrow window. The snapshot is sent before `broadcastPresence`, so it's in place before this client's connection can receive any chat message naming another member's clientID.
- `OnLeave` runs for every member that leaves this room instance (covers explicit leave *and* grace-period expiry — see [5](#5-voluntary-vs-involuntary-exit)).
- `OnDispose` runs once, when the room closes (membership hits zero) — nothing to clean up here.

Chat itself needs no server-side handler at all: `roommanager`'s built-in `CmdRoomMessage` command already forwards a `sendMessage` call to every other member of the room.

### 2.5 Serving `index.html` — and why `wss://`

A browser that loads the page over `https://` is only allowed to open `wss://` sockets (not `ws://`) — it's the same "mixed content" rule that applies to `<img>`/`fetch`. Rather than teaching a setup that breaks the moment you deploy behind TLS in production, this example already runs with TLS from the start, using a self-signed development certificate.

`main.go` spins up a second `http.Server` (`serveStatic`), just to serve `index.html`, the mascot, and the JS client itself (`/vendor/` → `clients/js/dist`, built locally by `make chat` — see [3.1](#31-importing-the-client)), on a port separate from the WebSocket port:

```go
--8<-- "examples/chat/main.go:static-server"
```

### 2.6 `main.go` — assembling the server

`main.go` assembles everything in a straight line, no jumping to another file to see what a callback does:

1. `newChatServer()`, then a `*knet.ConnectHooks` with two inline closures — connect logging, and disconnect logging + clearing the client's name — wired to `ws.Config` via `hooks.DispatchConnect`/`hooks.DispatchDisconnect` (`ws.WithTLS` enables `wss://`).
2. Once the `ws.Server` exists, `roommanager.New(server, hooks, roommanager.Config{})` — it self-registers connect/disconnect tracking on the same `hooks`.
3. `chat.roomManager.Define("lobby", func() roommanager.RoomHandler { return &lobbyRoom{names: chat.names} })` — registers the room type from 2.4. The factory runs once per room instance, not once per client.
4. `cfg.OnResume` — a single-slot field, not a `ConnectHooks` listener — wired to `roomManager.HandleResume` so a reconnect within the grace period resumes previous room membership.
5. The static file server, then `server.Start(ctx)` and graceful shutdown. No custom command handlers left to register — the display name goes in through the join itself (2.4), not a separate handler.

```go
--8<-- "examples/chat/main.go:bootstrap"
```

The certificate (`cert.pem`/`key.pem`) is generated automatically by `make chat-example` — see [Running the example](#6-running-the-example).

## 3. Web client, step by step

The client is a single `index.html`, with no build step of its own. It uses the official [`@lcaetano/knet-client`](js-client.md) client and its [`RoomManager`](js-client.md#rooms) — no reimplementing the wire format, reconnection, or room protocol by hand.

### 3.1 Importing the client

```js
--8<-- "examples/chat/index.html:import"
```

`/vendor/index.js` is the client's own ESM build (`clients/js/dist/index.js`), served by `main.go` (2.5) — `make chat` runs `npm run build` in `clients/js` first, so this always reflects the client code actually in this repo, with no CDN or npm-publish delay in the loop. A `<script type="module">` is all that's needed — no bundler, no `node_modules` in `examples/chat` itself.

### 3.2 Command IDs on the client

```js
--8<-- "examples/chat/index.html:commands"
```

Only the two presence commands are custom — `RoomManager` (constructed as `new RoomManager(client)`) owns join/leave/message/name internally, the name passed as `joinRoom`'s metadata (3.3).

### 3.3 Wiring it all up in the UI

```js
--8<-- "examples/chat/index.html:ui"
```

Important points:

- on `connected`, the client calls `rooms.joinRoom(ROOM_ID, ROOM_TYPE, { name })`, resending the name on every connect — initial or reconnect (see [4](#4-reconnection-in-practice)); `ROOM_TYPE` ("lobby") must match the key passed to `roomManager.Define` on the server (2.4), or the first join for a room has no `RoomHandler` factory to run
- `rooms.sendMessage(ROOM_ID, "chat", text)` replaces a raw `client.send(...)` call — it's scoped to the room, not broadcast to the whole server
- `rooms.on("message", ...)` delivers **other** members' chat messages, tagged with `senderId` — `sendMessage` is never echoed back to its own sender, so the "Enviar" click handler renders the sender's own message locally instead of waiting for one that will never arrive; the client keeps a small local `clientId -> name` map (populated from `UserJoined`/`UserLeft`) to render a name instead of a raw ID for everyone else's messages
- `rooms.on("memberDisconnected", ...)` reports an involuntary drop for another member, sourced from `roommanager`'s grace-period tracking
- `client.disconnect()` turns off auto-reconnect and closes with a normal close frame, which makes the server see `voluntary = true`

## 4. Reconnection in practice

When the connection drops and reconnects automatically, the server assigns a **new `clientID`** to the handshake, and the client-side `RoomManager` clears `currentRooms` on every reconnect (see [Room Manager → reconnect caveat](room-manager.md#client-libraries)) — the client has no way yet to prove it's "the same user" as before. That's why the `connected` handler always re-joins `"lobby"` with the name attached, both on the initial connection and on every reconnect: skip it and the client stops receiving room messages entirely, with no name known to the server either.

## 5. Voluntary vs. involuntary exit

This is the chat's trickiest requirement, and `roommanager` solves it for you via its grace-period membership:

| User action | What the server sees | Event emitted |
|---|---|---|
| Clicks "Leave" | Normal WebSocket close frame | `CmdRoomMemberEvent` type `left` (+ `UserLeft`) |
| Closes the browser tab | A close frame is usually sent by the browser too | `left` in most cases |
| Loses network connection (Wi-Fi drops, process frozen, cable unplugged) | No close frame arrives; the server detects it via read timeout | `CmdRoomMemberEvent` type `disconnected`, membership pending for `GraceTTL` |
| Client process is killed (`kill -9`, crash) | No close frame | `disconnected`, same grace period |
| Client reconnects within `GraceTTL` | `OnResume` fires with the previous room list | `CmdRoomMemberEvent` type `reconnected`, membership restored automatically |

There's no application code deciding this — it's the `voluntary` parameter of `OnDisconnect` (section [2.6](#26-maingo-assembling-the-server)) that carries this information, computed by the WebSocket protocol itself, and `roommanager.HandleDisconnect`/`HandleResume` that turn it into the right membership state machine. `lobbyRoom.OnLeave` (2.4) fires the same way whether the leave was explicit or a grace-period expiry — it doesn't need to know which.

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
