<!-- harness:start -->
# Project profile (consumed by /pr and /ticket)

- REPO: luciancaetano/knet
- DEFAULT_BRANCH: main
- TRACKER: none (GitHub Issues fallback)
- TICKET_PREFIX: empty
- TRACKER_BROWSE_URL: empty
- TRACKER_CLOSE_KEYWORD: Closes
- PR_TOOL: gh
- COMMIT_TYPES: feat | fix | refactor | chore | perf | docs | test (gitmoji-prefixed, see root CLAUDE.md)
- STACK: go (root module), node-ts (clients/js)
- LINT_CMD: make lint (golangci-lint run ./...); clients/js: none configured (tsc --noEmit via npm run typecheck)
- UNIT_TEST_CMD: make test-unit (./tests/unit/...); package-level: go test ./<pkg>/... ; clients/js: npm test
- E2E_TEST_CMD: make test-e2e (./tests/e2e/...); stress: make test-stress (needs ulimit -n 65536 first)
- BUILD_CMD: go build ./...; clients/js: npm run build (tsup)
- SETUP_QUIRKS: `make chat` runs `npm install && npm run build` in clients/js before the Go example; `make docs` builds a local .venv on first run for mkdocs-material
- ASSUMPTIONS:
  - No ticket tracker prefix found in last 50 commits or branch names; /pr and /ticket should use GitHub issue refs (`Closes #N`) if a ticket is referenced.
  - Per-area agent rules live in per-area `CLAUDE.md` (not `AGENTS.md`), per explicit user instruction on 2026-09-15. No per-area CLAUDE.md files exist yet; only the root one.
  - clients/js has no lint script in package.json (only build/typecheck/test); routing this as "no LINT_CMD configured, use typecheck as the closest gate" is a low-confidence guess.
<!-- harness:end -->
