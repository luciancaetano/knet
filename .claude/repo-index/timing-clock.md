---
id: timing-clock-deep-index
targets:
  - "timing/**/*"
  - "clock/**/*"
---

# Deep Index: Timing + Clock (Go)

> `timing/` is the tick scheduler and RTT ping/pong. `clock/` is a separate, newer package (added
> alongside the roommanager rework, see git history `84c46bd`) for clock sync; keep the two
> distinct, do not merge scheduling logic into `clock/` or vice versa.

## 1. Domain Component Mapping
- **Tick scheduler:** `timing/time_manager.go`.
- **RTT ping/pong:** `timing/ping.go`.
- **Clock sync:** `clock/clock.go`.
- **Test fakes:** `timing/fakes_test.go`.

## 2. Local Architecture Gotchas
- **Risk Zones:** `timing/ping.go` RTT measurement and `clock/clock.go` sync are easy to conflate
  since both deal with server/client time; check which one a change actually targets before
  editing, they solve different problems (round-trip latency vs synchronized wall-clock).

## 3. Local Verification Loop
- **Test:** `go test ./timing/... ./clock/... -run TestName -v`.
- **Lint:** `make lint`. **Build:** `go build ./...`.

## 4. Documentation Map
- No dedicated doc file; behavior defined by package source and tests.
