# JS Client

`@lcaetano/knet-client` — browser WebSocket client for the knet protocol. Zero runtime dependencies. Mirrors the [Unity client](unity-client.md) feature-for-feature.

## Install

```bash
npm install @lcaetano/knet-client
```

## Quick start

```ts
import { KNetClient } from "@lcaetano/knet-client";

const client = new KNetClient({ url: "ws://localhost:8080/ws" });

client.on("connected", () => console.log("connected"));
client.on("disconnected", (code, reason) => console.log("disconnected", code, reason));
client.on("error", (msg) => console.error(msg));

client.onCommand(0x0001, (payload) => {
  console.log("server says:", new TextDecoder().decode(payload));
});

await client.connect();
await client.sendString(0x0001, "hello server!");
```

## Sending

```ts
await client.send(0x0001, new Uint8Array([1, 2, 3])); // raw bytes
await client.sendString(0x0001, "hello");              // UTF-8 string
await client.sendJson(0x0001, { score: 42 });           // JSON.stringify
```

## Receiving

```ts
client.onCommand(0x0100, (payload: Uint8Array) => {
  // handle
});
```

## JSON-RPC 2.0

```ts
const result = await client.callRpc<{ score: number }>("getScore", { userId: 42 });
```

Rejects after 30s with no response, or immediately if the server returns a JSON-RPC error object — matches the server's [`RegisterJSONRPCHandler` / `HandleJSONRPC`](server-api.md#json-rpc-pattern-requestresponse).

## Reconnect

```ts
new KNetClient({
  url: "ws://localhost:8080/ws",
  autoReconnect: true,       // default true
  reconnectDelayMs: 3000,    // delay × min(attempt, 5)
  maxReconnectAttempts: 0,   // 0 = unlimited
  connectionTimeoutMs: 10000,
});
```

Backoff schedule with the default `reconnectDelayMs = 3000`:

| Attempt | Delay |
|---|---|
| 1 | 3s |
| 2 | 6s |
| 3 | 9s |
| 4 | 12s |
| 5+ | 15s |

`client.disconnect()` closes the socket and permanently disables reconnect.

## Wire format

```
[1B version][4B BE commandID][payload, up to 10 MiB]
```

User-defined command IDs must stay below `0xFFFFFFFC` — see [Reference](reference.md#reserved-command-ids) for the reserved range.

Raw WebSocket keepalive pings (`websocket.PingMessage`) are handled by the underlying `WebSocket` implementation; they aren't application-level command IDs and need no custom handling in this client.
