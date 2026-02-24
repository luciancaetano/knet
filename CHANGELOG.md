# Changelog

All notable changes to knet are documented here.
Format follows [Keep a Changelog](https://keepachangelog.com/en/1.0.0/).

---

## [Unreleased] — Security & Performance Hardening

This release is the result of a full security and performance audit.  It
contains **breaking API changes** and a number of behaviour changes.  Read the
migration notes below before upgrading.

---

### Breaking Changes

#### `OnConnectFn` now returns `bool`  *(HIGH-4)*

```go
// Before
type OnConnectFn = func(client knet.Client)

// After
type OnConnectFn = func(client knet.Client) bool
```

Return `true` to accept the connection; return `false` to reject it
immediately with a WebSocket `1008 Policy Violation` close code.

**Migration** — add `return true` to every existing `onConnect` callback:

```go
// Before
ws.NewConfig(addr, rl, origin, func(c knet.Client) {
    trackClient(c)
}, onDisconnect)

// After
ws.NewConfig(addr, rl, origin, func(c knet.Client) bool {
    trackClient(c)
    return true          // or return false to reject
}, onDisconnect)
```

#### `BroadcastCommand` may now return a non-nil error  *(LOW-7)*

Previously `BroadcastCommand` always returned `nil`.  It now returns an error
when one or more client deliveries fail (e.g. full send buffer).  If your code
ignores the return value, no change is needed.

#### `NewClient` signature changed  *(PERF-5)*

`NewClient` now accepts a `pingInterval time.Duration` parameter.  This is an
internal function; it only affects code that constructs clients directly rather
than through the server.

---

### Security Fixes

#### CRIT-1 — Concurrent WebSocket write (data race)

`CloseWithCode` previously called `conn.WriteControl` directly while `writePump`
might simultaneously call `conn.WriteMessage`, violating gorilla/websocket's
"one concurrent writer" invariant.

**Fix:** `CloseWithCode` no longer writes to the connection. It delivers the
close-frame payload to `writePump` via a dedicated `closeCh` channel. `writePump`
is now the sole writer for its connection's entire lifetime, including the
close handshake. A priority-check idiom ensures the close frame is sent before
any pending normal messages.

#### CRIT-2 — Unrecovered panics crash the server

Handler goroutines (`go handlerFunc(...)`, `go handleJSONRPCMessage(...)`)
had no `recover()`. A panic in any application handler would kill the server
process.

**Fix:** All handler dispatch now goes through `dispatchHandler`, which wraps
every task in a `defer recover()`. Panics are logged with a full stack trace
(`runtime/debug.Stack()`); the goroutine exits cleanly and the server continues.

#### CRIT-3 — No TLS support

The server only supported plain `ws://`. All traffic was unencrypted.

**Fix:** `ServerConfig` now has `TLSCertFile`, `TLSKeyFile`, and `TLSConfig`
fields. When `TLSCertFile` and `TLSKeyFile` are provided, the server uses
`(*http.Server).ServeTLS` to enable `wss://`.

```go
cfg := &websocket.ServerConfig{
    Addr:        ":8443",
    TLSCertFile: "/etc/tls/server.crt",
    TLSKeyFile:  "/etc/tls/server.key",
}
```

#### HIGH-1 — No connection limit (DoS)

No cap on concurrent connections allowed file-descriptor and goroutine
exhaustion from a single attacker.

**Fix:** `ServerConfig.MaxConnections int64` — set to `0` (default) for
unlimited. When the cap is reached, new upgrade requests receive
`503 Service Unavailable` before a connection is accepted.

#### HIGH-2 — WebSocket read limit not set (memory exhaustion)

The gorilla WebSocket library buffered the entire frame into memory before
the protocol layer could check the size, allowing a 10 MB (or larger) frame to
be allocated per connection before rejection.

**Fix:** `conn.SetReadLimit(MaxPayloadSize + HeaderSize)` is now called
immediately after upgrading. gorilla closes over-sized frames with
`1009 Message Too Big` without allocating the full buffer.

#### HIGH-3 — Unbounded goroutine creation per message

Every incoming message spawned an untracked goroutine (`go handlerFunc(...)`),
creating O(clients × msg/s) goroutines per second under load.

**Fix:** A fixed-size worker pool processes all handler tasks. Size is
configurable via `ServerConfig.WorkerCount` (default: `GOMAXPROCS × 4`) and
`ServerConfig.WorkerQueueSize` (default: 10 000). When the queue is full, the
task is dropped with a log line rather than spawning an unbounded goroutine.

#### HIGH-4 — No authentication rejection mechanism

`OnConnectFn` had no return value, so there was no API-enforced way to reject
a connection after the WebSocket handshake. Developers had to call
`client.Close()` manually, creating a window where unauthenticated clients
could send messages.

**Fix:** `OnConnectFn` now returns `bool` (see Breaking Changes above).

#### MED-1 — `Send` held `RLock` while blocking on channel

`Send` acquired `c.mu.RLock()` and kept it while blocking on `sendCh <- data`.
Under broadcast load with a full send buffer, this starved `Close` (which
needs `Lock`) indefinitely, preventing clean shutdown.

**Fix:** The lock is released immediately after snapshotting the `closed`
flag; the channel send proceeds without any lock held.

#### MED-2 — Context cancellation could not interrupt blocked `ReadMessage`

The previous `select { case <-ctx.Done(): ... default: ReadMessage() }` pattern
was misleading: once execution entered `ReadMessage`, a context cancellation
could not interrupt it.

**Fix:** A lightweight watcher goroutine sets `conn.SetReadDeadline(time.Now())`
when `client.Context()` is cancelled, causing any blocked `ReadMessage` to
return immediately with an error.

#### MED-3 — `Stop` did not drain handler goroutines

After `Stop` returned, handler goroutines spawned before the shutdown might
still be running, accessing application resources that had been released.

**Fix:** `Stop` now waits in order:
1. All `handleClient` goroutines (`clientWg.Wait()`).
2. All in-flight handler tasks (`handlerWg.Wait()`).
3. Worker pool shutdown (`close(workerCh)`).
4. HTTP listener shutdown (`server.Shutdown`).

After `Stop` returns, no knet-owned goroutine is running.

#### MED-4 — Internal errors leaked to clients via JSON-RPC

`err.Error()` from handler functions was sent verbatim in JSON-RPC error
responses, potentially exposing database connection strings, internal paths, or
other sensitive details.

**Fix:** The full error is logged server-side; the client receives only the
generic `"Internal error"` message defined in `knet.ErrInternalError`.

#### MED-5 — No HTTP server timeouts (Slowloris)

The HTTP server had no `ReadHeaderTimeout`, making it vulnerable to Slowloris
attacks on the WebSocket upgrade handshake.

**Fix:** HTTP server timeouts are now set:
- `ReadHeaderTimeout`: 10 s
- `ReadTimeout`: 15 s
- `WriteTimeout`: 15 s
- `IdleTimeout`: 120 s

#### MED-6 — `Decode` returned an aliased slice

`protocol.Decode` returned a sub-slice of the input buffer. Handlers that
retained the payload across goroutines could observe mutations from other parts
of the read pipeline.

**Fix:** `Decode` now returns an independent copy (`make` + `copy`). The
comment warning callers not to modify the slice has been removed.

#### MED-7 — Rate limit applied to message count only

The default burst of 200 × 10 MB = 2 GB could be transferred in a single
burst before rate limiting engaged, because the limiter counted messages, not
bytes.

**Partial fix:** HIGH-2 (transport-level read limit) reduces the practical
impact. A dedicated byte-budget rate limiter is planned for a future release.

#### LOW-1 — Voluntary disconnect flag was always inaccurate

`onDisconnect`'s `voluntary` parameter was derived from
`client.Context().Err() == context.Canceled`, but `cancel()` is called for
both server-initiated and client-initiated closes, making `voluntary` always
`true` for rate-limit disconnects.

**Fix:** `Client` now carries a `serverClosed atomic.Bool`. It is set to
`true` before any server-initiated `CloseWithCode` call. The `voluntary`
argument passed to `onDisconnect` is `!client.serverClosed.Load()`.

#### LOW-3 — Startup race (100 ms timer)

`Start` used a 100 ms timer to detect bind errors. On a slow or loaded system
this could return `nil` while the server goroutine silently failed.

**Fix:** `net.Listen` is called synchronously before launching the serve
goroutine. Bind errors surface immediately as a returned error from `Start`.

#### LOW-5 — No per-IP connection limiting

A single IP could exhaust the global connection limit before legitimate users
could connect.

**Fix:** `ServerConfig.MaxConnPerIP int64` — set to `0` (default) for
unlimited. Per-IP counts are tracked with a `sync.Map` of `*atomic.Int64`.
Requests exceeding the cap receive `429 Too Many Requests`.

#### LOW-6 — `AllOrigins()` had no production-safe alternative

The only built-in origin validator allowed every origin, enabling cross-site
WebSocket hijacking.

**Fix:** `ws.AllowedOrigins(origins []string) CheckOriginFn` — accepts
connections only from the provided list (case-insensitive, trailing-slash
normalised):

```go
ws.AllowedOrigins([]string{
    "https://game.example.com",
    "https://staging.example.com",
})
```

`AllOrigins()` now documents that it is for local development only.

#### LOW-7 — `BroadcastCommand` silently discarded errors

`BroadcastCommand` always returned `nil` regardless of delivery failures.

**Fix:** Returns a non-nil error reporting the count of failed deliveries.

---

### Performance Improvements

#### PERF-1 — Broadcast re-encoded payload per client

`BroadcastCommand` called `protocol.Encode` once per client, creating N
allocations for N clients on every broadcast.

**Fix:** `Encode` is called once; the resulting `[]byte` is shared across all
client send channels (safe because `writePump` only reads the bytes).

#### PERF-5 — Ping interval was hard-coded

The 54-second ping interval was hard-coded in `writePump` and could not be
changed without forking the library.

**Fix:** `ServerConfig.PingInterval time.Duration` — defaults to 54 s. The
value is propagated to each `Client` at creation time.

#### PERF-6 — Slow clients blocked broadcast

`Send` (used in the old `BroadcastCommand`) blocked on a full send channel,
holding an `RLock` and preventing `Close` from running on the slow client.

**Fix:** `BroadcastCommand` now uses `sendRaw`, a non-blocking variant of
`Send`. A full buffer causes that client's copy to be dropped (with a log
entry) rather than stalling the entire broadcast. The returned error reports
the total drop count.

---

### Internal Improvements

- `protocol.HeaderSize` and `protocol.MaxPayloadSize` are now exported so
  transport layers can reference them without duplicating magic numbers.
- `extractIP`, `incrIPCount`, `decrIPCount` helpers added for per-IP tracking.
- `dispatchHandler` centralises panic recovery, WaitGroup tracking, and worker
  dispatch in one place.
- Pre-existing test bugs in `server_test.go` fixed (wrong expected addresses).

---

### Migration Summary

| Change | Action required |
|---|---|
| `OnConnectFn` returns `bool` | Add `return true` (or auth logic) to every callback |
| `BroadcastCommand` may return error | Check the error if delivery guarantees matter |
| `NewConfig` / `NewClient` signature | `NewClient` is internal; `NewConfig` unchanged in parameter count |
| `Decode` returns a copy | Remove any code that relied on the payload aliasing the read buffer |

