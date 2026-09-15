# Server API

## Configuration

```go
ws.NewConfig(addr, rateLimit, checkOrigin, onConnect, onDisconnect)
```

| Param | Type | Notes |
|-------|------|-------|
| `addr` | `string` | listen address, e.g. `":8080"` |
| `rateLimit` | `*ws.RateLimitConfig` | `ws.DefaultRateLimitConfig()`, `ws.NoRateLimit()`, or `&ws.RateLimitConfig{MessagesPerSecond, Burst, Enabled}` |
| `checkOrigin` | `ws.CheckOriginFn` | `ws.AllOrigins()` or `func(*http.Request) bool` |
| `onConnect` / `onDisconnect` | `func(knet.Client)` | optional, `nil` allowed |

```go
checkOrigin := func(r *http.Request) bool {
	allowed := map[string]bool{"https://yourdomain.com": true}
	return allowed[r.Header.Get("Origin")]
}
config := ws.NewConfig(":8080", ws.DefaultRateLimitConfig(), checkOrigin, nil, nil)
server := ws.New(config)
```

### Optional setup

Chain these on the config before `ws.New`:

```go
config = ws.WithLogger(config, myLogger)   // knet.Logger
config = ws.WithMetrics(config, myMetrics) // knet.Metrics
config = ws.WithTLS(config, "cert.pem", "key.pem")
// or bring your own *tls.Config:
config = ws.WithTLSConfig(config, tlsCfg)
```

## Starting and stopping

```go
server := ws.New(config)
// ... register handlers ...

ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
defer stop()

if err := server.Start(ctx); err != nil {
	log.Fatal(err)
}
```

`Start` blocks until `Stop` is called or `ctx` is cancelled. `Stop(ctx)` closes all client connections gracefully.

!!! note
    Register every handler **before** calling `Start()`.

## Message handlers

### Command pattern (fire-and-forget)

Async, own goroutine per message. Use for game events, chat, notifications:

```go
server.RegisterHandler(ctx, 0x0100, func(client knet.Client, payload []byte) {
	position := processMovement(payload)
	client.Send(ctx, 0x0100, position)
	server.BroadcastCommand(ctx, 0x0101, position)
})
```

`RegisterHandler(ctx, commandID uint32, handler knet.HandlerFunc) error` — `handler` is `func(client knet.Client, payload []byte)`.

!!! note
    `payload` is zero-copy — don't retain or modify it past the handler call. Handlers run concurrently; don't assume ordering between them.

### JSON-RPC pattern (request/response)

Synchronous, follows [JSON-RPC 2.0](https://www.jsonrpc.org/specification). Use for queries, config, auth.

```go
server.RegisterJSONRPCHandler(ctx, "player.getStats", func(params map[string]interface{}) (interface{}, error) {
	playerID, ok := params["playerId"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid playerId")
	}
	return getPlayerStats(playerID), nil
})
```

Type-safe alternative — `knet.HandleJSONRPC` unmarshals `params` into a concrete `Req` struct and marshals the return value, instead of manual `map[string]interface{}` assertions:

```go
type MoveRequest  struct{ X, Y float64 `json:"x,y"` }
type MoveResponse struct{ OK bool     `json:"ok"` }

knet.HandleJSONRPC(ctx, server, "player.move",
	func(req MoveRequest) (MoveResponse, error) {
		return MoveResponse{OK: movePlayer(req.X, req.Y)}, nil
	},
)
```

A JSON decode failure is returned to the caller as a JSON-RPC invalid-request error automatically.

## Client interface

Handlers receive a `knet.Client`:

| Method | Description |
|--------|-------------|
| `ID() string` | unique client/session ID |
| `Send(ctx, commandID uint32, payload []byte) error` | send one message to this client |
| `ConnectionPayload() knet.ConnectionPayload` | snapshot of the HTTP handshake that opened this connection — headers, query params, cookies, path, user-agent, negotiated subprotocol |

### ConnectionPayload

A read-only snapshot captured once, at the WebSocket upgrade. Not live — it never reflects anything that happens after the handshake, and carries no TLS info or HTTP method/proto (fixed by definition).

| Method | Description |
|--------|-------------|
| `GetHTTPHeader(name string) (string, error)` | one header value, or `knet.ErrHeaderNotFound` |
| `GetParam(name string) (string, error)` | one query string param, or `knet.ErrParamNotFound` |
| `GetCookie(name string) (string, error)` | one cookie, or `knet.ErrCookieNotFound` |
| `Headers() http.Header` | all handshake headers |
| `Query() url.Values` | all query string params |
| `Path() string` | handshake URL path, e.g. `/ws` |
| `UserAgent() string` | `User-Agent` header, or `""` |
| `Subprotocol() string` | negotiated `Sec-WebSocket-Protocol`, or `""` |

```go
func onConnect(client knet.Client) bool {
	token, err := client.ConnectionPayload().GetHTTPHeader("Authorization")
	if err != nil {
		return false // reject: no auth header
	}
	return validateToken(token)
}
```

## Broadcasting

| Method | Scope |
|--------|-------|
| `server.BroadcastCommand(ctx, commandID, payload)` | every connected client |
| `room.Room.Broadcast(ctx, commandID, payload)` | clients in one room — see [Rooms & Observer](rooms-observer.md) |
| `observer.Set.Broadcast(ctx, subject, commandID, payload)` | clients in a room that pass interest conditions |

## Security & limits

| Feature | Default | Notes |
|---------|---------|-------|
| Max payload | 10 MB | prevents OOM |
| Read timeout | 60s | renewed per message |
| Write timeout | 10s | prevents slow clients from blocking |
| Ping interval | 54s | keepalive, auto pong detection |
| Rate limit | 100 msg/s, burst 200 | per-client, token bucket |
| Origin check | none (must configure) | `ws.CheckOriginFn` |

A client over the rate limit is closed with WebSocket close code `1008` (Policy Violation).

## Sessions (reconnect/resume)

`knet.SessionStore` persists which room IDs a session was in, keyed by session ID — not game state or message history:

```go
type SessionStore interface {
	Get(sessionID string) (rooms []string, ok bool)
	Put(sessionID string, rooms []string, ttl time.Duration)
	Delete(sessionID string)
}
```

Default implementation is in-memory. Supply your own (Redis, etc.) to share sessions across multiple server instances.
