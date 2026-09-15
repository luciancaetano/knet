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

After editing any `.go` file, run `make fmt` (`go fmt ./...`) and `make lint` (`golangci-lint run ./...`) before finishing the task.

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

Body (optional): why, not what — wrap ~72 cols. Footer: issue refs, `BREAKING CHANGE:`.
