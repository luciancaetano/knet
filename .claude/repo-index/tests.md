---
id: tests-deep-index
targets:
  - "tests/**/*"
---

# Deep Index: Tests (`tests/`, Go)

> Cross-package/integration coverage, separate from each package's own colocated `*_test.go`.
> `tests/stress` is a separate Go build (own `go.mod` context implied by `make test-stress`
> requiring a raised `ulimit`), the other two share the root module's test run.

## 1. Domain Component Mapping
- **Unit:** `tests/unit/` cross-package unit coverage, run via `make test-unit`.
- **E2E:** `tests/e2e/` integration coverage over real connections, run via `make test-e2e`.
- **Stress:** `tests/stress/` load/stress tests, run via `make test-stress` (needs
  `ulimit -n 65536` first, `-timeout 30m`).

## 2. Local Architecture Gotchas
- **Risk Zones:** running `go test ./tests/stress/...` directly from repo root instead of
  `cd tests/stress && go test -v -timeout 30m` (the `make test-stress` target) skips the required
  file-descriptor ulimit bump and the extended timeout, causing spurious failures under load.

## 3. Local Verification Loop
- **Test:** `make test` (all), `make test-unit`, `make test-e2e`, `make test-stress` (see Risk
  Zones), or `go test ./tests/unit/... -run TestName -v` for a single test.
- **Coverage:** `make test-coverage` writes `coverage.out` + `coverage.html`.
- **Lint:** `make lint`. **Build:** `go build ./...`.

## 4. Documentation Map
- Root `Makefile` is the authoritative command list for this area.
