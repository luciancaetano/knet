# Reference

## Wire format

Binary command protocol: 4-byte command ID (`uint32`, big-endian) + payload, zero-copy decode on the server.

```
[4B BE commandID][payload]
```

`CommandID 0x01` + `"hello"` → `[0x00,0x00,0x00,0x01,'h','e','l','l','o']`.

The JS/Unity client wire frame additionally prefixes a 1-byte version:

```
[1B version][4B BE commandID][payload, up to 10 MiB]
```

Split your application's command ID space by feature, e.g. `0x0100-0x01FF` player actions, `0x0200-0x02FF` chat. User-defined command IDs must stay below `0xFFFFFFFC`.

## Reserved command IDs

Do not register handlers on these — they're used internally by the server, JS client, and Unity client:

| ID | Constant (Go) | Purpose |
|----|----|---------|
| `0xFFFFFFFF` | `knet.CmdJSONRPC` | JSON-RPC 2.0 request/response |
| `0xFFFFFFFE` | `knet.CmdJSONRPCError` | JSON-RPC 2.0 error response |
| `0xFFFFFFFD` | `knet.CmdInvalidCommand` / `timing.PingCommandID` | reserved for "unrecognised command" replies; also used by `TimeManager.EnablePing` for RTT ping/pong |
| `0xFFFFFFFC` | `knet.CmdError` | reserved for "command processing error" replies (not currently emitted) |

## Errors

### Protocol / connection errors (Go constants)

| Constant | Message |
|---|---|
| `knet.ErrInvalidMessageFormat` | Invalid message format |
| `knet.ErrUnknownCommand` | unknown command |
| `knet.ErrParseError` | Parse error |
| `knet.ErrInvalidRequest` | Invalid Request |
| `knet.ErrMethodNotFound` | Method not found |
| `knet.ErrInternalError` | Internal error |
| `knet.ErrClientNotFound` | client not found |
| `knet.ErrConnectionClosed` | client connection is closed |
| `knet.ErrContextCancelled` | client context cancelled |
| `knet.ErrFailedToEncode` | failed to encode message |
| `knet.ErrServerAlreadyRunning` | server already running |

### JSON-RPC 2.0 error codes

Standard codes per the [JSON-RPC 2.0 spec](https://www.jsonrpc.org/specification):

| Code | Constant | Meaning |
|---|---|---|
| `-32700` | `knet.JSONRPCParseError` | invalid JSON was received |
| `-32600` | `knet.JSONRPCInvalidRequest` | JSON sent is not a valid request object |
| `-32601` | `knet.JSONRPCMethodNotFound` | method does not exist |
| `-32602` | `knet.JSONRPCInvalidParams` | invalid method parameters |
| `-32603` | `knet.JSONRPCInternalError` | internal server error |

`syncvar.Decode` additionally returns `syncvar.ErrTypeMismatch` when a payload's tag doesn't match the expected `syncvar.Tag`.

### WebSocket close codes

| Code | Meaning |
|---|---|
| `1008` | Policy Violation — client exceeded the configured rate limit |

## Security & limits

| Feature | Default |
|---------|---------|
| Max payload | 10 MB |
| Read timeout | 60s (renewed per message) |
| Write timeout | 10s |
| Ping interval | 54s |
| Rate limit | 100 msg/s, burst 200, per client |
| Origin check | none — must configure `ws.CheckOriginFn` |

## Packages

| Package | Purpose |
|---|---|
| `github.com/luciancaetano/knet` | core server interface, handlers, JSON-RPC helpers, sessions |
| `github.com/luciancaetano/knet/ws` | WebSocket server implementation, config |
| `github.com/luciancaetano/knet/room` | client groups for scoped broadcasting |
| `github.com/luciancaetano/knet/observer` | interest-managed broadcast on top of rooms |
| `github.com/luciancaetano/knet/syncvar` | dirty-tracked state sync |
| `github.com/luciancaetano/knet/timing` | fixed-rate tick loop + RTT ping |
| `@knet/client` (npm) | browser client, `clients/js/` |
| Unity client | `clients/unity/Client.cs` |

Full generated API docs: [pkg.go.dev/github.com/luciancaetano/knet](https://pkg.go.dev/github.com/luciancaetano/knet).
