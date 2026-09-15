---
id: room-deep-index
targets:
  - "internal/room/**/*"
  - "roommanager/**/*"
  - "spec/**/*"
---

# Deep Index: Room / roommanager (Go)

> Two layers: `internal/room/` is the low-level named-client-group primitive (scoped broadcast,
> a client can join multiple rooms); `roommanager/` is the higher-level public API built on it
> (join-time metadata on `OnJoin`, room snapshot on join, its own protocol/handler). Design
> reference for the manager layer: `spec/spec-architecture-room-manager.md`.

## 1. Domain Component Mapping
- **Room primitive:** `internal/room/room.go` core room type, membership, broadcast.
- **Manager public API:** `roommanager/roommanager.go`, `roommanager/handler.go`,
  `roommanager/protocol.go`.
- **Design doc:** `spec/spec-architecture-room-manager.md` architecture this was built against;
  read before changing `roommanager/` shape.

## 2. Local Architecture Gotchas
- **Risk Zones:** `roommanager` is public API and versioned per the "join-time metadata on
  OnJoin, room snapshot on join" breaking change (see git history `06d7eb0`); changing its
  wire protocol (`roommanager/protocol.go`) without updating `clients/js/src/room-manager.ts` and
  bumping the JS client version breaks the JS consumer silently.
- **Strict Patterns:** `internal/room/` stays internal (no consumer imports it directly); new
  public room-facing features go through `roommanager/`, not by exporting `internal/room` types.

## 3. Local Verification Loop
- **Test:** `go test ./internal/room/... ./roommanager/... -run TestName -v`.
- **Lint:** `make lint`. **Build:** `go build ./...`.
- **E2E:** `make test-e2e`.

## 4. Documentation Map
- `spec/spec-architecture-room-manager.md` design reference.
