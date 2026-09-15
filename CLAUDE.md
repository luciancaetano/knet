# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```bash
make test              # all tests (./tests/...)
make test-unit         # ./tests/unit/... only
make test-e2e          # ./tests/e2e/... only
make test-stress       # stress tests, needs `ulimit -n 65536` first
make test-coverage     # coverage.out + coverage.html
make fmt               # go fmt ./...
make lint              # golangci-lint run ./...
make chat              # run examples/chat locally
make docs              # serve mkdocs site at :8037 (installs .venv/mkdocs-material on first run)
```

Single test: `go test ./tests/unit/... -run TestName -v` (swap the path for `./tests/e2e/...` etc). Package-level unit tests also live alongside source, e.g. `go test ./room/... -run TestName -v`.

After editing any `.go` file, run `make fmt` (`go fmt ./...`) and `make lint` (`golangci-lint run ./...`) before finishing the task. `make lint` runs against the whole repo, not just touched packages — treat every issue it reports as in scope to fix, including pre-existing ones unrelated to the current change, not just newly introduced ones. Validation (build/vet/lint/test) is a whole-package gate, not scoped to the diff.

## Architecture

Binary command-pattern WebSocket protocol: every message is a 4-byte big-endian `CommandID` (`uint32`) followed by a raw payload, decoded zero-copy. Optional JSON-RPC 2.0 sits on top for request/response flows, multiplexed over three reserved command IDs (`0xFFFFFFFF`/`0xFFFFFFFE`/`0xFFFFFFFD` — see README "Reserved IDs"; never register handlers on these).

Root package `knet` (files `kephasnet.go`, `commands.go`, `session.go`, `helpers.go`, `logger.go`, `metrics.go`) defines the core interfaces only — `Server`, `Client`, `ConnectionPayload`. The actual WebSocket implementation lives in `ws/` (`ws.New(config)`), built on `github.com/gorilla/websocket`. This interface/implementation split is why `ws` imports `knet` but core logic (handler dispatch, rate limiting via `golang.org/x/time`, origin checks, TLS, logging, metrics) is implemented in `ws`, not in the root package.

Everything past the core protocol is opt-in and independently composable — each hooks into the server only via `Client`/`onConnect`/`onDisconnect`, not by modifying `ws`:
- `room/` — named client groups for scoped broadcast (lobbies, matches, chat channels); a client can be in multiple rooms.
- `observer/` — interest-managed broadcast (`observer_conditions.go` filters who receives what).
- `syncvar/` — dirty-tracked state sync (see `spec/spec-architecture-room-manager.md` for the design this was built against).
- `timing/` — tick scheduler + RTT ping/pong (`ping.go`, `time_manager.go`).

Test layout mirrors this split: `tests/unit`, `tests/e2e`, `tests/stress` (separate build) at the top level for cross-package/integration coverage; each package (`room`, `observer`, `syncvar`, `timing`, `ws/*`) also carries its own `*_test.go` and `fakes_test.go` for in-package unit tests.

`examples/` holds runnable consumers per client target: `chat/` (Go server + `js/` client over `wss://` with a generated dev cert, run via `make chat`), `unity/` (Unity client), `metrics-prometheus/`, `stress-echo/`, `wss-echo/`.

Docs: `doc.go` is the package-level godoc (protocol format, quick start). `docs-site/` + `mkdocs.yml` build the mkdocs site (`make docs`, served at https://luciancaetano.github.io/knet/); README.md is the canonical protocol/config/API reference — check it before re-deriving usage examples.

## JS client (clients/js)

Any change to the JS client bumps its version following [Semantic Versioning](https://semver.org) (MAJOR.MINOR.PATCH: MAJOR = breaking API, MINOR = backward-compatible feature, PATCH = backward-compatible fix). After bumping the version, run `npm install` in that package to regenerate the lockfile hashes.

Client (`clients/js`) and server (`knet` Go module) versions track the same MAJOR.MINOR — only PATCH moves independently between them.

## Commit & push policy

Always ask the user for confirmation before running `git commit` or `git push` — never do either without an explicit go-ahead first.

## Commits

Format: `<gitmoji> <type>(<scope>): <description>`

- type/scope/description follow [Conventional Commits](https://www.conventionalcommits.org): `feat`, `fix`, `chore`, `docs`, `refactor`, `test`, `perf`, `ci`, `build`, `revert`. Scope optional, lowercase, matches touched package/dir.
- description: imperative mood, lowercase start, no period.
- Breaking change: append `!` after type/scope (`feat(api)!: ...`) and add `BREAKING CHANGE:` footer explaining it.
- gitmoji picked by intent, most-used here:
  - ✨ `:sparkles:` feat
  - 🐛 `:bug:` fix
  - 📝 `:memo:` docs
  - ♻️ `:recycle:` refactor
  - ✅ `:white_check_mark:` tests
  - ⚡️ `:zap:` perf
  - 🔧 `:wrench:` config
  - 🚀 `:rocket:` deploy/release
  - 🔥 `:fire:` remove code/files
  - 💚 `:green_heart:` fix CI
  - ⬆️ `:arrow_up:` / ⬇️ `:arrow_down:` deps
  - 🔒️ `:lock:` security fix

Full list: https://gitmoji.dev

Example:
```
✨ feat(chat): add reconnect backoff to ws client
```

Body (optional): why, not what, wrap ~72 cols. Footer: issue refs, `BREAKING CHANGE:`.

<!-- harness:start -->
## Navigation index

> Read this first; do not blind-recurse the repo. Deep per-area detail lives in
> `.claude/repo-index/*.md`, read the one matching the area you touch (not auto-loaded). Per-area
> agent rules live in a per-area `CLAUDE.md` (not `AGENTS.md`) when one exists; none exist yet
> outside this root file.

### 1. System Topology
- **Core protocol `./` (root package `knet`):** Go, interfaces only (`Server`, `Client`,
  `ConnectionPayload`) plus the wire command-ID format.
- **WS transport `ws/` + `internal/websocket/` + `internal/protocol/`:** Go, `ws/` is a thin
  public facade re-exporting `internal/websocket/`, the real gorilla-websocket implementation.
- **Room `internal/room/` + `roommanager/`:** Go, low-level room primitive plus the public
  roommanager API built on it.
- **Observer `observer/`:** Go, interest-managed broadcast.
- **Syncvar `syncvar/` + `clients/js/src/syncvar.ts`:** Go + JS, dirty-tracked state sync, both
  sides must stay wire-compatible.
- **Timing/Clock `timing/` + `clock/`:** Go, tick scheduler/RTT ping-pong vs. clock sync (distinct
  concerns, do not conflate).
- **Examples `examples/`:** Go (+ JS/Unity), runnable consumers per client target.
- **JS client `clients/js/`:** TypeScript, `@lcaetano/knet-client`, MAJOR.MINOR tracks the Go
  module.
- **Tests `tests/`:** Go, cross-package unit/e2e/stress suites (each package also has its own
  colocated `*_test.go`).
- **Docs `docs-site/` + `spec/`:** mkdocs site source; `spec/spec-architecture-room-manager.md` is
  the roommanager design reference.

### 2. Structural Routing Triggers
- Adding/changing a public interface or the command-ID format: `./` (root package).
- Handler dispatch, rate limiting, origin checks, TLS, connection lifecycle: `internal/websocket/`
  (re-export new public surface via `ws/server.go`).
- Room membership/broadcast primitive: `internal/room/`; room-facing public API/protocol:
  `roommanager/`.
- Interest-managed/filtered broadcast: `observer/`.
- Dirty-tracked state sync: `syncvar/` (mirror in `clients/js/src/syncvar.ts`).
- Tick scheduling or RTT ping: `timing/`; wall-clock sync: `clock/`.
- Browser client behavior: `clients/js/src/`.
- Cross-package/integration test: `tests/unit/`, `tests/e2e/`, `tests/stress/`.

### 3. Search & Grep Optimization
- **File patterns:** `*.go` (root + packages), `clients/js/src/*.ts`, `examples/**/main.go`.
- **Deny rules:** `.venv/`, `docs-site/` build output, `node_modules/` (under `clients/js/`),
  `clients/js/dist/`, `.git/`, `.claude/worktrees/`.
- **Non-code zones:** `docs-site/docs/`, `spec/`, `README.md`, `ROADMAP.md`, `llms-full.txt`.

### 4. Deep Index Pointers
- Editing `./*.go` (root files): read `.claude/repo-index/core-protocol.md`.
- Editing `ws/**`, `internal/websocket/**`, `internal/protocol/**`: read
  `.claude/repo-index/ws-transport.md`.
- Editing `internal/room/**`, `roommanager/**`, `spec/**`: read `.claude/repo-index/room.md`.
- Editing `observer/**`: read `.claude/repo-index/observer.md`.
- Editing `syncvar/**` or `clients/js/src/syncvar.ts`: read `.claude/repo-index/syncvar.md`.
- Editing `timing/**`, `clock/**`: read `.claude/repo-index/timing-clock.md`.
- Editing `examples/**`: read `.claude/repo-index/examples.md`.
- Editing `clients/js/**`: read `.claude/repo-index/clients-js.md`.
- Editing `tests/**`: read `.claude/repo-index/tests.md`.

### 5. Deeper navigation (on-demand)
- `.claude/meta/navigation.md` for the doc map, instruction hierarchy, and agent/skill guide.
- `.claude/harness/profile.md` for /pr and /ticket project tokens.
<!-- harness:end -->
