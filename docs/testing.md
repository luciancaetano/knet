# Docker stress & e2e testing

Besides the in-process tests in `tests/unit`, `tests/e2e` and `tests/stress`,
this repo has a Docker-based track that runs the built server binary in an
isolated container over a real network stack.

Pieces:

- `Dockerfile` — multi-stage build of `examples/stress-echo` (a minimal
  plain-WS echo server) into a small alpine image, with a TCP healthcheck.
- `docker-compose.yml` — runs that image, exposing the server on host port
  `9000`.
- `tests/stress/docker_stress_test.go` — build-tag `docker` Go tests that
  connect from the host to the containerized server: concurrent
  throughput/latency, and a reconnect-churn/goroutine-leak sanity check.
- `tests/docker-e2e.sh` — brings the compose stack up, waits for it to be
  healthy, round-trips a real message against it, then tears it down.

## Running

```bash
# start the containerized server
docker compose up -d --build

# stress tests from the host, against the container
cd tests/stress
KNET_STRESS_ADDR=localhost:9000 go test -tags docker -run TestDockerStress -timeout 5m -v ./...
cd ../..

# full e2e (up, wait healthy, round-trip, down) in one shot
./tests/docker-e2e.sh

# tear down manually if needed
docker compose down -v
```

`tests/stress` is its own Go module (see its `go.mod`), so `-tags docker`
tests there don't affect `go build ./...` / `go vet ./...` / `go test ./...`
at the repo root.
