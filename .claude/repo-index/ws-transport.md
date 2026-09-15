---
id: ws-transport-deep-index
targets:
  - "ws/**/*"
  - "internal/websocket/**/*"
  - "internal/protocol/**/*"
---

# Deep Index: WebSocket transport (`ws/`, `internal/websocket/`, `internal/protocol/`, Go)

> `ws/` is a thin public facade (type aliases + `New(cfg)` re-exports) over `internal/websocket/`,
> which holds the real implementation on `github.com/gorilla/websocket`. `internal/protocol/`
> carries the wire encode/decode. Handler dispatch, rate limiting (`golang.org/x/time`), origin
> checks, TLS, logging, and metrics wiring all live in `internal/websocket/`, not `ws/`.

## 1. Domain Component Mapping
- **Public facade:** `ws/server.go` re-exports `websocket.ServerConfig`, `RateLimitConfig`,
  `CheckOriginFn`, `OnConnectFn`/`OnDisconnectFn`/`OnResumeFn`, and `New()`/`With*` builders.
- **Server implementation:** `internal/websocket/websocket_server.go` connection accept loop,
  handler dispatch, rate limiting, origin checks, TLS, metrics/logging hooks.
- **Client implementation:** `internal/websocket/websocket_client.go` per-connection read/write,
  resume handling.
- **Session:** `internal/websocket/session.go`.
- **Wire encode/decode:** `internal/protocol/protocol.go` the 4-byte big-endian `CommandID` +
  payload framing.

## 2. Local Architecture Gotchas
- **Risk Zones:** adding a new public config knob only in `internal/websocket/websocket_server.go`
  without re-exporting the matching type alias / `With*` builder in `ws/server.go` makes it
  unreachable from outside the module; always mirror new public surface into `ws/server.go` (see
  the existing alias block there).
- **Risk Zones:** `OnConnectFn` returns `bool` (accept/reject with policy-violation close), a
  documented breaking change baked into the type alias comment in `ws/server.go`; treat any new
  connect-gating hook the same way rather than reintroducing a void callback.
- **Strict Patterns:** keep `ws/server.go` limited to type aliases and thin constructor wrappers;
  put actual logic in `internal/websocket/` (mirror `ws/server.go`).

## 3. Local Verification Loop
- **Test:** `go test ./ws/... ./internal/websocket/... ./internal/protocol/... -run TestName -v`.
- **Lint:** `make lint`. **Build:** `go build ./...`.
- **E2E:** `make test-e2e` (./tests/e2e/...) exercises this layer over real connections.

## 4. Documentation Map
- Root `README.md` "Reserved IDs" section and protocol/config reference.
