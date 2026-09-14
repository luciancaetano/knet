# Getting Started

## 1. Install

```bash
go get github.com/luciancaetano/knet
```

Go 1.21+ required (generics, `context`).

## 2. Create a server

```go
package main

import (
	"context"
	"log"

	"github.com/luciancaetano/knet"
	"github.com/luciancaetano/knet/ws"
)

func main() {
	ctx := context.Background()

	config := ws.NewConfig(
		":8080",                     // listen address
		ws.DefaultRateLimitConfig(), // 100 msg/s, burst 200 per client
		ws.AllOrigins(),             // ⚠ dev only — see Server API
		func(client knet.Client) bool { client.Send(ctx, 0x0001, []byte("welcome")); return true },
		func(client knet.Client, voluntary bool) { log.Printf("disconnected: %s (voluntary=%v)", client.ID(), voluntary) },
	)

	server := ws.New(config)

	// Register a handler before Start()
	server.RegisterHandler(ctx, 0x0100, func(client knet.Client, payload []byte) {
		client.Send(ctx, 0x0100, []byte("login ok"))
	})

	log.Fatal(server.Start(ctx))
}
```

Run it:

```bash
go run .
```

## 3. Connect a client

Any WebSocket client works if it speaks the [wire format](reference.md#wire-format). The maintained option is `@knet/client` for JS/TS:

```bash
npm install @knet/client
```

```ts
import { KNetClient } from "@knet/client";

const client = new KNetClient({ url: "ws://localhost:8080/ws" });

client.onCommand(0x0100, (payload) => {
  console.log(new TextDecoder().decode(payload)); // "login ok"
});

await client.connect();
await client.sendString(0x0100, "");
```

See the [JS Client](js-client.md) page for the full API, or `clients/unity/Client.cs` for the Unity client (same wire contract).

## 4. Test with wscat

No client library handy? Use `wscat` and send raw bytes — a command ID is 4 bytes big-endian, e.g. `0x00000100`:

```bash
npm install -g wscat
wscat -c ws://localhost:8080/ws
```

## Next steps

- [Server API](server-api.md) — full config reference, handler patterns, security defaults
- [Rooms & Observer](rooms-observer.md) — group clients for scoped broadcasts
- [SyncVar & Timing](syncvar-timing.md) — tick-rate state replication for game loops
- Working programs: [`examples/`](https://github.com/luciancaetano/knet/tree/main/examples) (`wss-echo`, `stress-echo`, `metrics-prometheus`)
