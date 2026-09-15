# knet

<img src="assets/knet-dog.png" alt="knet mascot" width="180">

Go library for building game servers and real-time apps over WebSocket. Binary command-pattern protocol (4-byte command ID + payload) with optional JSON-RPC 2.0.

<div class="grid cards" markdown>

- **[Introduction](introduction.md)** — what knet is, why it exists, performance & cost
- **[Getting Started](getting-started.md)** — install, first server, first client
- **[Server API](server-api.md)** — config, handlers, JSON-RPC, security limits
- **[Rooms & Observer](rooms-observer.md)** — scoped broadcasting, interest management
- **[SyncVar & Timing](syncvar-timing.md)** — tick loop, dirty-tracked state sync, RTT
- **[JavaScript Client](js-client.md)** — browser client for the knet protocol
- **[Unity Client](unity-client.md)** — C# client for Unity games
- **[Reference](reference.md)** — reserved command IDs, error table, wire format

</div>

## Install

```bash
go get github.com/luciancaetano/knet
```

## 60-second example

```go
package main

import (
	"context"
	"log"

	"github.com/luciancaetano/knet"
	"github.com/luciancaetano/knet/ws"
)

const EchoCmd uint32 = 0x0001

func main() {
	ctx := context.Background()

	server := ws.New(ws.NewConfig(
		":8080",
		ws.DefaultRateLimitConfig(),
		ws.AllOrigins(), // dev only — see Server API for production origin checks
		nil, nil,
	))

	server.RegisterHandler(ctx, EchoCmd, func(client knet.Client, payload []byte) {
		client.Send(ctx, EchoCmd, payload)
	})

	log.Fatal(server.Start(ctx))
}
```

Connect with a client. Both echo `EchoCmd` (`0x0001`) back and forth with the server above.

=== "JavaScript"

    ```bash
    npm install @lcaetano/knet-client
    ```

    ```ts
    import { KNetClient } from "@lcaetano/knet-client";

    const client = new KNetClient({ url: "ws://localhost:8080/ws" });

    client.onCommand(0x0001, (payload) => {
      console.log("server says:", new TextDecoder().decode(payload));
    });

    await client.connect();
    await client.sendString(0x0001, "hello server!");
    ```

    See the full [JavaScript Client](js-client.md) guide for reconnect, JSON-RPC, and more.

=== "Unity (C#)"

    ```csharp
    var go = new GameObject("KNetClient");
    var client = go.AddComponent<Knet.Client>();
    client.Config.url = "ws://localhost:8080/ws";

    client.On(0x0001, payload =>
        Debug.Log($"server says: {System.Text.Encoding.UTF8.GetString(payload)}"));

    await client.ConnectAsync();
    await client.SendStringAsync(0x0001, "hello server!");
    ```

    See the full [Unity Client](unity-client.md) guide for SyncVars, JSON-RPC, and more.

Any other WebSocket client works too, as long as it speaks the [wire format](reference.md#wire-format).
