---
id: clients-js-deep-index
targets:
  - "clients/js/**/*"
---

# Deep Index: JS client (`clients/js`, TypeScript)

> Browser WebSocket client for the knet protocol, published as `@lcaetano/knet-client`. Tracks the
> same MAJOR.MINOR as the Go server module; only PATCH moves independently between them.

## 1. Domain Component Mapping
- **Entry:** `clients/js/src/index.ts`.
- **Core client:** `clients/js/src/client.ts`.
- **Wire protocol:** `clients/js/src/protocol.ts`.
- **JSON-RPC layer:** `clients/js/src/jsonrpc.ts`.
- **Room manager mirror:** `clients/js/src/room-manager.ts`, `clients/js/src/room-protocol.ts`
  (mirrors Go `roommanager/`, see `[[room]]`).
- **Syncvar mirror:** `clients/js/src/syncvar.ts` (mirrors Go `syncvar/`, see `[[syncvar]]`).
- **Build:** `tsup` (`package.json` scripts: `build`, `typecheck`, `test`).

## 2. Local Architecture Gotchas
- **Risk Zones:** editing this package without bumping `version` in `clients/js/package.json`
  (per SemVer: MAJOR = breaking API, MINOR = backward-compatible feature, PATCH = fix) and running
  `npm install` afterward to regenerate lockfile hashes, per root `CLAUDE.md` "JS client" section.
- **Risk Zones:** `package.json` has no `lint` script, only `build`/`typecheck`/`test`; do not
  assume a lint gate exists here, `typecheck` (`tsc --noEmit`) is the closest verification step.
- **Strict Patterns:** any protocol change here must be mirrored against the matching Go package
  (`roommanager/`, `syncvar/`) in the same change, see `[[room]]` and `[[syncvar]]` deep indexes.

## 3. Local Verification Loop
- **Test:** `cd clients/js && npm test` (`node --test src/*.test.ts`).
- **Lint:** none configured; use **Typecheck:** `cd clients/js && npm run typecheck`.
- **Build:** `cd clients/js && npm run build`. **E2E:** none dedicated.

## 4. Documentation Map
- `clients/js/package.json` scripts and version. Root `CLAUDE.md` "JS client (clients/js)"
  section for the version-bump policy.
