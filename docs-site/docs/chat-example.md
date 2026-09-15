# Chat Example (JS)

Full, step-by-step tutorial for building a multi-user chat with `room` (Go) on the server and a plain HTML/JavaScript client. The code in this tutorial is real and runs — it's in [`examples/chat/`](https://github.com/luciancaetano/knet/tree/main/examples/chat) in the repository.

## 1. Overview

We'll build:

- a Go server that groups all clients into a `room` called `"lobby"`
- broadcasting of chat messages to everyone in the room
- automatic notifications of who joins and who leaves
- automatic reconnection on the client side
- the most important piece: distinguishing when someone **left on purpose** (clicked "Leave") from when they **dropped unexpectedly** (network went down, tab froze, timeout)

### Protocol

| ID | Direction | Payload | Purpose |
|---|---|---|---|
| `0x0001` SetName | client → server | text (desired name) | sets/updates the name on connect or reconnect |
| `0x0002` Chat | client → server → room | JSON `{"from","text"}` | chat message |
| `0x0003` UserJoined | server → room | JSON `{"name"}` | someone joined |
| `0x0004` UserLeft | server → room | JSON `{"name"}` | someone left **voluntarily** |
| `0x0005` UserDisconnected | server → room | JSON `{"name"}` | someone dropped **unexpectedly** |

## 2. Go server, step by step

The server is organized into 4 files, each with a single responsibility — not one monolithic `main.go`:

```
examples/chat/
├── protocol.go   # command IDs and payload types (JSON)
├── names.go      # name storage keyed by clientID
├── server.go     # chatServer: OnConnect/OnDisconnect and handlers
└── main.go       # bootstrap: assembles the ws.Server and registers everything
```

### 2.1 `protocol.go` — Command IDs and payloads

We define the chat protocol's IDs as constants, along with the JSON formats used in the payloads. Application IDs must stay below `0xFFFFFFFC` — that range is reserved by knet (see [Reference](reference.md#reserved-command-ids)).

```go
--8<-- "examples/chat/protocol.go:commands"
```

### 2.2 `names.go` — Storing each client's name

`knet.Client` doesn't store application state — only connection identity (`ID()`, `RemoteAddr()`, etc). So the server keeps its own `clientID -> name` map, isolated in a dedicated type (`nameStore`) and protected by a mutex, since handlers run on concurrent goroutines.

```go
--8<-- "examples/chat/names.go:names"
```

### 2.3 `server.go` — the `chatServer` type

All the chat logic lives in methods of a single type, `chatServer`, which holds the `room` and the `nameStore`. This avoids loose variables captured by scattered closures — each handler is just a method with explicit access to the state it needs.

```go
--8<-- "examples/chat/server.go:server-type"
```

A single `room.Room` called `"lobby"` already gives us everything we need: `Add`, `Remove`, `Broadcast` — no manual loops over clients, the room's `Broadcast` is already the optimized way to send to everyone.

### 2.4 `OnConnect` and `OnDisconnect` — the heart of the example

```go
--8<-- "examples/chat/server.go:connect"
```

`OnConnect` receives the `knet.Client` as soon as the WebSocket connection is accepted, and returns `bool`: `true` accepts the connection, `false` rejects it (closes with "policy violation"). Here we only add the client to the room — it doesn't have a name yet, which arrives next via `SetName`.

`OnDisconnect` is where the magic happens: `func(client knet.Client, voluntary bool)`. The `voluntary` parameter comes already computed by knet:

- `voluntary == true` → the client closed the connection normally (sent a real close frame — e.g., the user clicked "Leave" and the JS called `disconnect()`)
- `voluntary == false` → the connection dropped without a clean close (network outage, tab force-closed, read timeout)

We don't need any application message like `"I'm leaving"` — the server already knows the difference natively, and we use that directly to choose between `UserLeft` and `UserDisconnected`.

### 2.5 `SetName` handler

```go
--8<-- "examples/chat/server.go:setname-handler"
```

`UserJoined` is only emitted here, after the client sends a name — in `OnConnect` we still have no way to identify them in the UI.

### 2.6 `Chat` handler

```go
--8<-- "examples/chat/server.go:chat-handler"
```

Simple: grab the sender's name, wrap it in JSON with the text, and broadcast to the whole room — including the sender itself (so the sender also sees their message appear the same way as everyone else).

### 2.7 Serving `index.html` — and why `wss://`

A browser that loads the page over `https://` is only allowed to open `wss://` sockets (not `ws://`) — it's the same "mixed content" rule that applies to `<img>`/`fetch`. Rather than teaching a setup that breaks the moment you deploy behind TLS in production, this example already runs with TLS from the start, using a self-signed development certificate.

`main.go` spins up a second `http.Server` (`serveStatic`), just to serve `index.html` and the mascot, on a port separate from the WebSocket port:

```go
--8<-- "examples/chat/main.go:static-server"
```

### 2.8 `main.go` — assembling the server

`main.go` stays lean: it creates the `chatServer`, builds the `ws.Server` config with `ws.WithTLS` (enables `wss://`) pointing `OnConnect`/`OnDisconnect` at the `chatServer` methods, registers the two command handlers, starts the static server, and boots with graceful shutdown.

```go
--8<-- "examples/chat/main.go:bootstrap"
```

The certificate (`cert.pem`/`key.pem`) is generated automatically by `make chat-example` — see the [Running the example](#6-running-the-example) section.

## 3. Web client, step by step

The client is a single `index.html`, with no build step. It uses the official [`@lcaetano/knet-client`](js-client.md) client, loaded directly from a CDN via ESM — no reimplementing the wire format or reconnection by hand.

### 3.1 Importing the official client

```js
--8<-- "examples/chat/index.html:import"
```

`esm.sh` serves the package published on npm as a pure ES module, so all you need is a `<script type="module">` — no bundler, no `node_modules`. The wire format (`[1B version][4B BE commandID][payload]`) and automatic reconnection are already handled by `KNetClient` (see [JS Client](js-client.md#wire-format)).

### 3.2 Command IDs on the client

```js
--8<-- "examples/chat/index.html:commands"
```

### 3.3 Wiring it all up in the UI

```js
--8<-- "examples/chat/index.html:ui"
```

Important points:

- `client.connect()` opens the socket; `KNetClient` itself schedules automatic reconnection unless `client.disconnect()` was called beforehand
- `client.disconnect()` turns off auto-reconnect and closes with a normal close frame, which makes the server see `voluntary = true` — that's why the UI keeps `leftVoluntarily` only to decide which status message to show, not to control reconnection itself
- the client's default reconnection backoff is `delay = reconnectDelayMs × min(attempt, 5)` (see [JS Client → Reconnect](js-client.md#reconnect))
- `client.on("connected", ...)`, `client.on("disconnected", ...)`, and `client.onCommand(id, ...)` replace the manual wrapper's `addEventListener` calls

## 4. Reconnection in practice

When the connection drops and reconnects automatically, the server assigns a **new `clientID`** to the handshake — it has no memory that this is "the same user" as before. That's why the `connected` event always resends `SetName`, both on the initial connection and on every reconnect: without it, the server would have a client with no registered name, and subsequent chat messages would show up with the ID itself as the sender.

## 5. Voluntary vs. involuntary exit

This is the chat's trickiest requirement, and knet solves it for you:

| User action | What the server sees | Event emitted |
|---|---|---|
| Clicks "Leave" | Normal WebSocket close frame | `UserLeft` (`voluntary = true`) |
| Closes the browser tab | A close frame is usually sent by the browser too | `UserLeft` in most cases |
| Loses network connection (Wi-Fi drops, process frozen, cable unplugged) | No close frame arrives; the server detects it via read timeout | `UserDisconnected` (`voluntary = false`) |
| Client process is killed (`kill -9`, crash) | No close frame | `UserDisconnected` (`voluntary = false`) |

There's no application code deciding this — it's the `voluntary` parameter of `OnDisconnect` (section [2.4](#24-onconnect-and-ondisconnect-the-heart-of-the-example)) that carries this information, computed by the WebSocket protocol itself (whether or not a valid close frame was received).

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

- [Rooms & Observer](rooms-observer.md) — more than one room, interest management
- [JavaScript Client](js-client.md) — full reference for `KNetClient`: JSON-RPC, sync vars, configurable reconnection, and more
- [Server API](server-api.md) — full server configuration, rate limiting, security
