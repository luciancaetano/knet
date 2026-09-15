# knet Benchmark Suite

Automated performance suite measuring knet under increasing connection load,
with and without `RoomManager` (`room`) and `Ticker` (`timing`), plus a
dedicated Ticker jitter/drift test. Separate Go module so the charting
dependency (`gonum.org/v1/plot`) never touches the root module.

## What it measures

| Metric | How |
|---|---|
| Latency (p50/p95/p99) | Round-trip echo over the raw wire protocol |
| Memory / connection | `runtime.MemStats` heap diff before/after N connections, `runtime.GC()`'d |
| Throughput (msg/s) | Server-side `knet_msg_received_total` counter (via `knet.Metrics`), not client-side counting |
| Ticker jitter | Drift of actual tick fire time vs configured interval, `OnPreTick` |

Scenarios: `baseline` (no Room, no Ticker), `room`, `ticker`, `room_ticker`.

## Charts

Generated into `images/` (and copied to `docs-site/docs/assets/benchmark/`):

- **`latency.png`** — p99 round-trip latency (ms) vs connection count, one line per scenario. Shows how much Room/Ticker overhead adds to response time as load grows.
- **`memory_per_conn.png`** — heap bytes per connection vs connection count, one line per scenario. Isolates the fixed memory cost `room`/`timing` add per client on top of the base WebSocket connection.
- **`throughput.png`** — messages/sec the server sustains vs connection count, one line per scenario, measured server-side via `knet_msg_received_total`.
- **`ticker_jitter.png`** — p99 drift (ms) between a tick's actual fire time and its configured interval, vs connection count, comparing `no_room` (plain `Register`) against `room` (`RegisterRoom`, which also broadcasts). Shows whether tick scheduling stays stable as more clients (or a bigger room broadcast) compete for CPU time.

X axis is log-scaled (connections grow 100→10000).

> **Caveat:** the load-generating client (`benchClient`) and the `knet`
> server under test run in the same OS process, sharing the same cores.
> `throughput.png` flattening as connections grow past a few hundred
> reflects the combined process's CPU ceiling, not necessarily the server's
> — the client's own encode/dial/read-loop goroutines are competing for the
> same GOMAXPROCS. Treat the absolute throughput numbers as a lower bound on
> server capacity, not an exact measurement; a true isolated measurement
> needs the client driven from a separate process (skipped here — add if a
> real regression needs pinning down to server-only cost).

## Run everything

```bash
cd .benchmark
go run .
```

Max load level is 10000 connections. Higher was tested but capped: on a
single machine, all client connections dial `127.0.0.1` from the same source
IP, so each one needs a distinct ephemeral source port — capped by
`net.ipv4.ip_local_port_range` (default ~28k ports on Linux), well short of
50000. Raising it further would need widening that OS-level port range
(`sudo sysctl -w net.ipv4.ip_local_port_range="1024 65535"`) or spreading
clients across multiple loopback addresses — out of scope for this suite.

Writes CSV to `results/`, PNG charts to `images/` and
`../docs-site/docs/assets/benchmark/`.

## Run one scenario

```bash
go test -run TestScenarioRoom -v
go test -run TestTickerJitter -v
```

Heavy tests are skipped under `-short`.

## Override load levels

Default loads: `100, 500, 1000, 5000, 10000` (see the port-range note above for why it stops there).

```bash
go run . -loads 100,500,1000        # CLI flag
BENCH_LOADS=20,50 go test ./bench/  # env var, same effect
```

## Notes

- The suite is heavy and not part of `make test` — run it on demand.
- `results/*.csv` is gitignored (regenerated per run); `images/*.png` is
  committed, since the README/docs reference them statically.
