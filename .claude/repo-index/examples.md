---
id: examples-deep-index
targets:
  - "examples/**/*"
---

# Deep Index: Examples (Go + JS/Unity consumers)

> One runnable consumer per client target. Not library code; each subdir is a standalone
> `main.go` (or Unity project) demonstrating one integration path.

## 1. Domain Component Mapping
- **Chat example:** `examples/chat/` Go server + `js/` client over `wss://` with a generated dev
  cert (`cert.pem`/`key.pem`), run via `make chat` (builds `clients/js` first, then `go run .`).
- **Prometheus metrics:** `examples/metrics-prometheus/` implements the root `knet.Metrics`
  interface against Prometheus.
- **Stress echo:** `examples/stress-echo/` target for `make test-stress`.
- **Plain wss echo:** `examples/wss-echo/` minimal server.

## 2. Local Architecture Gotchas
- **Risk Zones:** running `examples/chat` directly with `go run .` before `clients/js` is built
  serves a stale or missing JS bundle; always go through `make chat`, which runs
  `npm install && npm run build` first.

## 3. Local Verification Loop
- **Test:** examples are runnable demos, not unit-tested; verify by running (`make chat`) or
  `go build ./examples/...`.
- **Lint:** `make lint`. **Build:** `go build ./...`.

## 4. Documentation Map
- Root `README.md` links each example's purpose.
