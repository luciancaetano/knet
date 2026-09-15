---
id: syncvar-deep-index
targets:
  - "syncvar/**/*"
  - "clients/js/src/syncvar.ts"
  - "clients/js/src/syncvar.test.ts"
---

# Deep Index: Syncvar (Go + JS client)

> Dirty-tracked state sync: server-side `syncvar/syncvar.go`, with a matching JS client
> implementation in `clients/js/src/syncvar.ts`. The two must stay wire-compatible.

## 1. Domain Component Mapping
- **Server:** `syncvar/syncvar.go` dirty-tracking and sync dispatch.
- **JS client mirror:** `clients/js/src/syncvar.ts` client-side decode/apply.

## 2. Local Architecture Gotchas
- **Risk Zones:** changing the dirty-tracking wire shape in `syncvar/syncvar.go` without updating
  `clients/js/src/syncvar.ts` in the same change breaks the JS client silently at runtime (no
  compile-time link between the two); update both together and bump the JS client version (see
  root `CLAUDE.md` "JS client" section).

## 3. Local Verification Loop
- **Test:** `go test ./syncvar/... -run TestName -v`; JS side: `cd clients/js && npm test`.
- **Lint:** `make lint`. **Build:** `go build ./...` / `cd clients/js && npm run build`.

## 4. Documentation Map
- No dedicated doc file; behavior defined by `syncvar/syncvar.go` and its tests.
