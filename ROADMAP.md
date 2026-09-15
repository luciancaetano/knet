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

- [ ] Session ID handshake: server never tells a client its assigned/resumed session ID, so neither client can resend `?session=` on reconnect — blocks true Room Manager resume-sync (REQ-020/AC-009, CON-005)
- [ ] Client version drift: JS client at 1.2.0, no visible Unity client version file — decide if Unity should track the same MAJOR.MINOR per the CLAUDE.md versioning rule
- [ ] Benchmark suite caps at 10k connections on one machine (per its README) — multi-machine/distributed load test is the natural ceiling to push next if scale claims need backing

## Out of scope (not planned)

- [x] ~~Docker-based testing/deployment~~ (deliberately dropped, `a2a8058`)
