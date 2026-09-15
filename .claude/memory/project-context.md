---
name: project-context
description: knet repo shape, verify commands, and setup quirks (from harness-init scan, 2026-09-15)
metadata:
  type: project
---

<!-- harness:start -->
Go module `github.com/luciancaetano/knet` (go 1.26) plus a TypeScript client package at
`clients/js` (`@lcaetano/knet-client`, tracks Go module's MAJOR.MINOR). No ticket tracker prefix
found in commit history, `/pr` and `/ticket` should fall back to GitHub issue refs.

Verify commands: `make lint` (whole-repo `golangci-lint run ./...`, not diff-scoped), `make test`
/ `make test-unit` / `make test-e2e`, `make test-stress` (needs `ulimit -n 65536` first), `go build
./...`. JS side: `cd clients/js && npm test` / `npm run typecheck` / `npm run build` (no `lint`
script configured there).

Setup quirks: `make chat` runs `npm install && npm run build` in `clients/js` before starting the
Go chat example, running the example directly first serves a stale JS bundle. `make docs`
bootstraps a local `.venv` with mkdocs-material on first run.

Per-area agent rules use per-area `CLAUDE.md` in this repo (not `AGENTS.md`), per explicit user
instruction; none exist yet outside the root file.
<!-- harness:end -->
