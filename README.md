# knet

[![Go Reference](https://pkg.go.dev/badge/github.com/luciancaetano/knet.svg)](https://pkg.go.dev/github.com/luciancaetano/knet)
[![Go Report Card](https://goreportcard.com/badge/github.com/luciancaetano/knet)](https://goreportcard.com/report/github.com/luciancaetano/knet)

A high-performance Go library for building game servers and real-time applications over WebSocket. Binary command-pattern protocol with optional JSON-RPC 2.0 support.

## Features

- Binary command protocol: 4-byte command ID (uint32 BE) + payload, zero-copy decode
- Optional JSON-RPC 2.0 handlers for request/response workflows
- Per-client rate limiting (token bucket)
- `OnConnect` / `OnDisconnect` lifecycle callbacks
- Broadcasting to all connected clients
- Built-in timeouts, payload limits, origin validation
- Opt-in packages: `room` (named client groups), `observer` (interest-managed broadcast), `syncvar` (dirty-tracked state sync), `timing` (tick scheduler + RTT ping)

## Install

```bash
go get github.com/luciancaetano/knet
```

## Quick Start

```go
package main

import (
	"context"
	"log"
	"time"

	"github.com/luciancaetano/knet"
	"github.com/luciancaetano/knet/ws"
)

func main() {
	ctx := context.Background()

	config := ws.NewConfig(
		":8080",
		ws.DefaultRateLimitConfig(), // 100 msg/s, burst 200
		ws.AllOrigins(),             // use a custom check in production
		func(client knet.Client) { // OnConnect
			log.Printf("connected: %s", client.ID())
			client.Send(ctx, 0x0001, []byte("welcome"))
		},
		func(client knet.Client) { // OnDisconnect
			log.Printf("disconnected: %s", client.ID())
		},
	)

	server := ws.New(config)

	err := server.RegisterHandler(ctx, 0x0100, func(client knet.Client, payload []byte) {
		log.Printf("login request: %s", string(payload))
		client.Send(ctx, 0x0100, []byte("login ok"))
	})
	if err != nil {
		log.Fatal(err)
	}

	log.Println("listening on :8080")
	if err := server.Start(ctx); err != nil {
		log.Fatal(err)
	}

	time.Sleep(10 * time.Minute)
	_ = server.Stop(ctx)
}
```

## Protocol

**Message format:**

```
┌────────────────┬──────────────────────┐
│  4 bytes        │  N bytes             │
│  CommandID      │  Payload             │
│  (uint32 BE)    │  (binary data)       │
└────────────────┴──────────────────────┘
```

`CommandID 0x01` + payload `"hello"` → `[0x00, 0x00, 0x00, 0x01, 'h', 'e', 'l', 'l', 'o']`

Each command ID routes to a registered handler. Split your ID space by feature, e.g. `0x0100-0x01FF` player actions, `0x0200-0x02FF` chat, `0x0300-0x03FF` inventory.

**Reserved command IDs** (JSON-RPC and ping/pong use these — do not register handlers on them):

| ID | Purpose |
|----|---------|
| `0xFFFFFFFF` | JSON-RPC request/response |
| `0xFFFFFFFE` | JSON-RPC error response |
| `0xFFFFFFFD` | Ping/pong (RTT, via `timing.TimeManager.EnablePing`) |

Everything else (`0x00000000`–`0xFFFFFFFC`) is available for your application.

## Configuration

`ws.NewConfig(addr, rateLimit, checkOrigin, onConnect, onDisconnect)`:

| Param | Type | Notes |
|-------|------|-------|
| `addr` | `string` | listen address, e.g. `":8080"` |
| `rateLimit` | `*ws.RateLimitConfig` | `ws.DefaultRateLimitConfig()`, `ws.NoRateLimit()`, or a custom `&ws.RateLimitConfig{MessagesPerSecond, Burst, Enabled}` |
| `checkOrigin` | `ws.CheckOriginFn` | `ws.AllOrigins()` or a `func(*http.Request) bool` |
| `onConnect` | `ws.OnConnectFn` | optional, `nil` allowed |
| `onDisconnect` | `ws.OnDisconnectFn` | optional, `nil` allowed |

> [!WARNING]
> Never use `ws.AllOrigins()` in production — it accepts connections from any origin.

**Production origin check:**

```go
checkOrigin := func(r *http.Request) bool {
	allowed := map[string]bool{
		"https://yourdomain.com":     true,
		"https://www.yourdomain.com": true,
	}
	return allowed[r.Header.Get("Origin")]
}
config := ws.NewConfig(":8080", ws.DefaultRateLimitConfig(), checkOrigin, nil, nil)
server := ws.New(config)
```

Optional post-config setup: `ws.WithLogger(cfg, logger)`, `ws.WithMetrics(cfg, metrics)`, `ws.WithTLS(cfg, certFile, keyFile)`.

## Message Handlers

knet supports two patterns:

**Command pattern** — async, fire-and-forget, runs in its own goroutine per message. Use for game events, chat, notifications, anything that doesn't need a guaranteed response:

```go
server.RegisterHandler(ctx, 0x0100, func(client knet.Client, payload []byte) {
	position := processMovement(payload)
	client.Send(ctx, 0x0100, position)
	server.BroadcastCommand(ctx, 0x0101, position)
})
```

**JSON-RPC pattern** — synchronous request/response, follows the [JSON-RPC 2.0 spec](https://www.jsonrpc.org/specification). Use for queries, config, auth:

```go
server.RegisterJSONRPCHandler(ctx, "player.getStats", func(params map[string]interface{}) (interface{}, error) {
	playerID, ok := params["playerId"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid playerId")
	}
	return getPlayerStats(playerID), nil
})
```

> [!NOTE]
> Payload slices reference the read buffer (zero-copy) — do not retain or modify them past the handler call. Handlers run concurrently; don't assume execution order. Register all handlers before `Start()`.

## Connection Lifecycle

`OnConnect` fires after handshake, `OnDisconnect` fires on close/error. Both run synchronously and are optional — avoid blocking work in either.

```go
var (
	mu      sync.RWMutex
	clients = make(map[string]knet.Client)
)

config := ws.NewConfig(":8080", ws.DefaultRateLimitConfig(), ws.AllOrigins(),
	func(client knet.Client) {
		mu.Lock()
		clients[client.ID()] = client
		mu.Unlock()
	},
	func(client knet.Client) {
		mu.Lock()
		delete(clients, client.ID())
		mu.Unlock()
	},
)
server := ws.New(config)
```

## Security & Limits

| Feature | Default | Notes |
|---------|---------|-------|
| Max payload | 10MB | prevents OOM |
| Read timeout | 60s | renewed per message |
| Write timeout | 10s | prevents slow clients from blocking |
| Ping interval | 54s | keepalive, auto pong detection |
| Rate limit | 100 msg/s, burst 200 | per-client, token bucket, `ws.RateLimitConfig` |
| Origin check | none (must configure) | `ws.CheckOriginFn` |

A client over the rate limit is closed with code `1008` (Policy Violation) and reason `"Rate limit exceeded"`.

## Rooms

The `room` package (opt-in) groups clients into named sets for scoped broadcasting — match instances, lobbies, zones, chat channels. Safe for concurrent use.

```go
import "github.com/luciancaetano/knet/room"

lobby := room.New("lobby-1")

config := ws.NewConfig(":8080", ws.DefaultRateLimitConfig(), ws.AllOrigins(),
	func(client knet.Client) { lobby.Add(client) },
	func(client knet.Client) { lobby.Remove(client.ID()) },
)
server := ws.New(config)

server.RegisterHandler(ctx, ChatCmd, func(client knet.Client, payload []byte) {
	lobby.BroadcastExcept(ctx, client.ID(), ChatCmd, payload)
})
```

| Method | Description |
|--------|-------------|
| `Add(client)` | insert a client (idempotent on reconnect) |
| `Remove(clientID)` | evict a client, no-op if absent |
| `Has(clientID)` | check membership |
| `Clients()` | snapshot of all clients in the room |
| `Size()` | number of clients currently in the room |
| `Broadcast(ctx, commandID, payload)` | send to everyone in the room |
| `BroadcastExcept(ctx, excludeID, commandID, payload)` | send to everyone but one client |
| `Close(ctx)` | remove and disconnect all clients |

A client can belong to multiple rooms at once — the server doesn't own rooms; your application does, via `room.New` per lobby/match/zone and membership tracked in `OnConnect`/`OnDisconnect`.

## Project Structure

```
knet/
├── kephasnet.go              # Public interfaces (Server, Client)
├── commands.go                # Constants (command IDs, errors)
├── session.go                 # SessionStore interface
├── logger.go, metrics.go      # Logger/Metrics interfaces + default no-op impls
├── doc.go                     # Package documentation
│
├── internal/                  # Implementation, not part of the public API
│   ├── protocol/               # Binary encode/decode
│   └── websocket/               # Server and client implementation
│
├── ws/                         # Public factory package (New, NewConfig, ...)
├── room/                       # Opt-in: named client groups
├── observer/                   # Opt-in: interest-managed broadcast
├── syncvar/                    # Opt-in: dirty-tracked state sync
├── timing/                     # Opt-in: tick scheduler + RTT ping
│
├── examples/                   # metrics-prometheus, stress-echo, wss-echo
└── tests/                      # unit, e2e, stress
```

## Testing

```bash
make test                                    # all tests
go test ./tests/unit/... -v                  # unit only
go test ./tests/e2e/... -v                   # end-to-end
go test ./tests/... -cover -coverprofile=coverage.out
```

Stress tests (5k/10k concurrent connections, throughput/latency benchmarks) live in `tests/stress`, see [`tests/stress/README.md`](tests/stress/README.md). For tests against the server in a real Docker container, see [`docs/testing.md`](docs/testing.md).

```bash
cd tests/stress
ulimit -n 65536              # raise FD limit first
go test -v -timeout 30m      # full suite, 15-30 min
go test -v -run TestStress5000Connections -timeout 10m
```

## License

MIT — see [LICENSE](LICENSE).
