# Roadmap

## Shipped

- [x] Binary command protocol (4-byte `CommandID` + payload, zero-copy decode) with optional JSON-RPC 2.0 overlay
- [x] `ws` server implementation: rate limiting, origin checks, TLS, timeouts, payload limits, Prometheus metrics
- [x] `room` — named client groups, scoped broadcast, parallelized tick fan-out (`1d750dc`)
- [x] `observer` — interest-managed broadcast with filter conditions
- [x] `syncvar` — dirty-tracked state sync, typed wire format
- [x] `timing` — tick scheduler + RTT ping/pong
- [x] JS client (`clients/js`, npm, v1.2.0) and Unity client (`clients/unity`)
- [x] Automated benchmark suite (`.benchmark/`) — latency, memory/connection, throughput, ticker jitter, CI-published charts
- [x] mkdocs site (`docs-site/`) mirroring README
- [x] **Room Manager** (`roommanager/`, per `spec/spec-architecture-room-manager.md`) — room lifecycle (create/join/leave/close), grace-period disconnect/reconnect, reserved command range, hooks, unit + e2e tests
- [x] Room Manager clients (spec §4.4/§4.5): `RoomManager` in JS (`clients/js`, v1.4.0) and Unity (`clients/unity`) — join/leave, membership events, typed protocol, room-scoped `sendMessage`/`OnMessage` (server: `Manager.OnRoomMessage` hook). Caveat: neither client persists a session ID across reconnect (CON-005), so reconnect always clears membership and fires `roomsLost`/`OnRoomsLost` instead of auto-resuming — true resume-sync needs a separate session-ID-handshake feature first

## Candidate next work

- [ ] Create a Clock Interface with, SetInterval, SetTimeout, ClearInterval, ClearTimeout, ElapsedTime, DeltaTime, CurrentTime, SetInterval and SetTimeout Returning ClockInstance with methods to pause, resume, clear, reset. and properties as elapsedtime, active, paused, its is a complete refactor of timing package and rename to clock package, the clock will be member of room as Clock() clock.Clock and clock package will export interface Clock and clock.new()

## Out of scope (not planned)

- [x] ~~Docker-based testing/deployment~~ (deliberately dropped, `a2a8058`)
