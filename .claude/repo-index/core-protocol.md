---
id: core-protocol-deep-index
targets:
  - "kephasnet.go"
  - "commands.go"
  - "session.go"
  - "helpers.go"
  - "logger.go"
  - "metrics.go"
  - "doc.go"
  - "*_test.go"
---

# Deep Index: Core protocol (root `knet` package, Go)

> Interface-only root package: `Server`, `Client`, `ConnectionPayload`, and the binary command-ID
> wire format. No transport implementation lives here; that is `ws/` + `internal/websocket/`.

## 1. Domain Component Mapping
- **Core interfaces:** `kephasnet.go` defines `Server`, `Client`, `ConnectionPayload`.
- **Command IDs:** `commands.go` defines the `uint32` `CommandID` type and the three reserved
  JSON-RPC multiplex IDs.
- **Session:** `session.go` small session-scoped type used by implementations.
- **Shared helpers:** `helpers.go` cross-cutting utilities used by `ws`/`internal` packages.
- **Logging/metrics contracts:** `logger.go`, `metrics.go` define `Logger`/`Metrics` interfaces
  implemented elsewhere (`examples/metrics-prometheus/`).
- **Package godoc:** `doc.go` protocol format and quick-start; canonical narrative reference is
  root `README.md`, not this file.

## 2. Local Architecture Gotchas
- **Risk Zones:** registering a handler on `0xFFFFFFFF`/`0xFFFFFFFE`/`0xFFFFFFFD` collides with the
  JSON-RPC multiplex reserved IDs (see `commands.go` and README "Reserved IDs") and silently breaks
  RPC request/response routing.
- **Strict Patterns:** this package holds interfaces and protocol constants only; do not add
  transport, handler-dispatch, or rate-limiting logic here, that belongs in `ws/`/`internal/websocket/`
  (mirror the split already in `kephasnet.go`).

## 3. Local Verification Loop
- **Test:** `go test ./... -run TestName -v` (root package tests are colocated `*_test.go`).
- **Lint:** `make lint`. **Build:** `go build ./...`. **E2E:** none at this layer.

## 4. Documentation Map
- `doc.go` package godoc. `README.md` canonical protocol/config/API reference, check before
  re-deriving usage examples. `llms-full.txt` AI-agent reference doc.
