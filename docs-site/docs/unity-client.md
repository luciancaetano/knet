# Unity Client

C# WebSocket client for the knet protocol. Mirrors the [JS client](js-client.md) feature-for-feature.

## Requirements

| Requirement | Minimum version |
|---|---|
| Unity | 2019.3 |
| Scripting backend | Mono or IL2CPP |
| API compatibility level | .NET Standard 2.0 or .NET 4.x |

`System.Net.WebSockets.ClientWebSocket` is built into the .NET runtime Unity 2019.3+ uses — no third-party WebSocket library needed.

## Install

Copy the five files from `clients/unity/` into any folder under your project's `Assets/` (e.g. `Assets/Plugins/Knet/`):

```
ConnectionState.cs
ReservedCommands.cs
Protocol.cs
Client.cs
SyncVarAttribute.cs
```

No Package Manager entry or `.asmdef` required. All types live in the `Knet` namespace.

## Quick start

### 1. Add the component

Do this manually in the Unity Editor:

1. In the **Hierarchy** window, right-click → **Create Empty**. Rename it to `KNetClient`.
2. With it selected, go to **Inspector** → **Add Component**.
3. Type `Client` in the search box and select **Client** (the `Knet` namespace component).
4. This object should persist across scene loads — tick a "don't destroy" flag if you use one, or call `DontDestroyOnLoad` from a small bootstrap script that runs once. A dedicated `NetworkManager` scene/prefab loaded first is the common pattern.

You now have a `Client` component sitting on a `GameObject`, ready to configure.

=== "Editor (Inspector)"

    Select the `KNetClient` GameObject and fill in the **Config** fields exposed by the `Client` component in the Inspector:

    | Field | Value |
    |---|---|
    | Url | `ws://localhost:8080/ws` |
    | Auto Reconnect | ✓ |
    | Reconnect Delay | `3` (seconds) |
    | Max Reconnect Attempts | `0` (unlimited) |
    | Connection Timeout | `10` (seconds) |
    | Debug | ✓ while developing |

    No code needed for configuration — these are serialized public fields on the component.

=== "Code"

    Grab a reference to the component (e.g. via `[SerializeField] private Client client;` dragged in the Inspector, or `GetComponent<Client>()`) and set the same fields before calling `ConnectAsync()`:

    ```csharp
    client.Config.url                  = "ws://localhost:8080/ws";
    client.Config.autoReconnect        = true;
    client.Config.reconnectDelay       = 3f;   // seconds (×1–5 backoff)
    client.Config.maxReconnectAttempts = 0;    // 0 = unlimited
    client.Config.connectionTimeout    = 10f;  // seconds
    client.Config.debug                = true; // verbose logging
    ```

### 2. Reference the component from your scripts

Add a `[SerializeField] private Client client;` field to any `MonoBehaviour` that needs the connection, then drag the `KNetClient` GameObject onto that field in the Inspector — no `AddComponent` call required at runtime:

```csharp
public class GameNetworkManager : MonoBehaviour
{
    [SerializeField] private Client client; // assign in Inspector

    private async void Start()
    {
        // client is already attached to the KNetClient GameObject and configured
        await client.ConnectAsync();
    }
}
```

### 3. Subscribe to events (optional but recommended)

All events fire on the Unity main thread.

```csharp
client.OnConnected    += () => Debug.Log("Connected!");
client.OnDisconnected += (code, reason) => Debug.Log($"Disconnected: {code} — {reason}");
client.OnError        += msg => Debug.LogError($"Error: {msg}");
```

### 4. Register handlers (before connecting)

```csharp
client.On(0x0001, payload =>
{
    string text = System.Text.Encoding.UTF8.GetString(payload);
    Debug.Log($"Server says: {text}");
});
```

### 5. Connect

```csharp
await client.ConnectAsync();
```

Returns once the handshake succeeds (or throws on failure). Receive loop and reconnect run in the background automatically.

### 6. Send

```csharp
await client.SendAsync(0x0001, new byte[] { 1, 2, 3 });      // raw bytes
await client.SendStringAsync(0x0001, "Hello server!");        // UTF-8 string
await client.SendJsonAsync(0x0001, new MyData { score = 42 }); // JsonUtility
```

### 7. Disconnect

```csharp
client.Disconnect(); // sends close frame; auto-reconnect disabled permanently
```

## SyncVars

Mirrors the server's [`syncvar.SyncVar`](syncvar-timing.md#syncvar-dirty-tracked-state-sync) — payloads are UTF-8 text matching the Go encoders.

```csharp
public class Position : MonoBehaviour
{
    private Client _client;

    [SyncVar(0x0010, SyncVarType.Float32)] private float _x;
    [SyncVar(0x0011, SyncVarType.Float32)] private float _y;

    private async void Start()
    {
        _client = gameObject.AddComponent<Client>();
        _client.BindSyncVars(this);   // bind BEFORE connecting
        await _client.ConnectAsync();
    }
}
```

Each `[SyncVar]` field is assigned from the incoming payload whenever its command ID arrives. `SyncVarType` supports `Int32`, `Int64`, `Float32`, `Float64`, `Bool`, `String`.

## JSON-RPC

```csharp
// paramsJson must be pre-serialised JSON.
string raw = await client.SendJsonRpcAsync("getScore", "{\"userId\": 42}");
var resp = JsonUtility.FromJson<MyResponse>(raw);

// Generic overload — parses "result" for you:
var score = await client.SendJsonRpcAsync<ScoreResult>("getScore", "{\"userId\": 42}");
```

`SendJsonRpcAsync` throws:

| Exception | When |
|---|---|
| `TimeoutException` | no response within 30 seconds |
| `Exception` | server returned a JSON-RPC error object |
| `InvalidOperationException` | client is not connected |

## JSON serialization notes

`Client` uses Unity's built-in `JsonUtility` internally, zero external dependencies. For complex types (dictionaries, anonymous objects, nested lists), use [Newtonsoft.Json for Unity](https://docs.unity3d.com/Manual/com.unity.nuget.newtonsoft-json.html) and call `SendStringAsync` directly:

```csharp
using Newtonsoft.Json;

var data = new Dictionary<string, object> { ["key"] = "value" };
await client.SendStringAsync(0x0001, JsonConvert.SerializeObject(data));

string paramsJson = JsonConvert.SerializeObject(new { userId = 42, room = "lobby" });
string raw = await client.SendJsonRpcAsync("joinRoom", paramsJson);
```

## Protocol reference

### Wire format

```
[4B BE commandID][payload, 0…10 MiB]
```

### Reserved command IDs

| Constant | Value | Purpose |
|---|---|---|
| `ReservedCommands.JsonRpc` | `0xFFFFFFFF` | JSON-RPC 2.0 envelope |
| `ReservedCommands.JsonRpcError` | `0xFFFFFFFE` | JSON-RPC error response |
| `ReservedCommands.InvalidCommand` | `0xFFFFFFFD` | unknown command (server → client) |
| `ReservedCommands.CommandError` | `0xFFFFFFFC` | command processing error |

User-defined command IDs must stay below `0xFFFFFFFC` — see [Reference](reference.md#reserved-command-ids).

### Reconnect backoff

```
delay = reconnectDelay × min(attempt, 5)
```

| Attempt | Delay (reconnectDelay = 3s) |
|---|---|
| 1 | 3s |
| 2 | 6s |
| 3 | 9s |
| 4 | 12s |
| 5+ | 15s |

## Full example

```csharp
using System;
using System.Threading.Tasks;
using Knet;
using UnityEngine;

public class GameNetworkManager : MonoBehaviour
{
    private Client _client;

    private async void Start()
    {
        _client = gameObject.AddComponent<Client>();
        _client.Config.url   = "ws://localhost:8080/ws";
        _client.Config.debug = true;

        _client.OnConnected    += () => Debug.Log("Connected to server.");
        _client.OnDisconnected += (code, reason) => Debug.Log($"Disconnected: {code} {reason}");
        _client.OnError        += msg => Debug.LogError($"Network error: {msg}");

        _client.On(0x0001, payload =>
            Debug.Log($"Chat: {System.Text.Encoding.UTF8.GetString(payload)}"));

        try
        {
            await _client.ConnectAsync();
            await _client.SendStringAsync(0x0001, "Hello from Unity!");
        }
        catch (Exception ex)
        {
            Debug.LogError($"Could not connect: {ex.Message}");
        }
    }

    private void OnDestroy() => _client?.Disconnect();
}
```
