# knet Unity Client

Unity C# WebSocket client for the **knet** protocol.

Mirrors the browser JS client (`examples/js-chat/kephas-client.js`) feature-for-feature.

---

## Requirements

| Requirement | Minimum version |
|---|---|
| Unity | 2019.3 |
| Scripting backend | Mono or IL2CPP |
| API compatibility level | **.NET Standard 2.0** or **.NET 4.x** |

`System.Net.WebSockets.ClientWebSocket` is built into the .NET runtime used by
Unity 2019.3+. No third-party WebSocket library is needed.

---

## Installation

Copy the four `.cs` files into any folder inside your Unity project's `Assets/`
directory (e.g. `Assets/Plugins/Knet/`):

```
ConnectionState.cs
ReservedCommands.cs
Protocol.cs
Client.cs
SyncVarAttribute.cs
```

No Unity Package Manager entry or `.asmdef` is required, though you may add one
if your project uses assembly definitions. All types live in the `Knet`
namespace.

---

## Quick Start

### 1 — Add the component

Attach `Client` to any persistent `GameObject` (e.g. a dedicated
`NetworkManager` object).

```csharp
// Or create one at runtime:
var go = new GameObject("KNetClient");
DontDestroyOnLoad(go);
var client = go.AddComponent<Client>();
```

### 2 — Configure

Via the Inspector **or** in code before calling `ConnectAsync()`:

```csharp
client.Config.url                  = "ws://localhost:8080/ws";
client.Config.autoReconnect        = true;
client.Config.reconnectDelay       = 3f;   // seconds (×1–5 backoff)
client.Config.maxReconnectAttempts = 0;    // 0 = unlimited
client.Config.connectionTimeout    = 10f;  // seconds
client.Config.debug                = true; // verbose logging
```

### 3 — Subscribe to events

All events are delivered on the **Unity main thread**.

```csharp
client.OnConnected    += () => Debug.Log("Connected!");
client.OnDisconnected += (code, reason) =>
    Debug.Log($"Disconnected: {code} — {reason}");
client.OnError        += msg => Debug.LogError($"Error: {msg}");
```

### 4 — Register message handlers

```csharp
// Handler is called on the main thread with the raw payload bytes.
client.On(0x0001, payload =>
{
    string text = System.Text.Encoding.UTF8.GetString(payload);
    Debug.Log($"Server says: {text}");
});
```

### 5 — Connect

```csharp
// From a MonoBehaviour coroutine / async method:
await client.ConnectAsync();
```

`ConnectAsync()` returns once the handshake succeeds (or throws on failure).
The receive loop and reconnect logic run in the background automatically.

### 6 — Send messages

```csharp
// Binary payload
await client.SendAsync(0x0001, new byte[] { 1, 2, 3 });

// UTF-8 string payload
await client.SendStringAsync(0x0001, "Hello server!");

// JsonUtility-serialisable object
await client.SendJsonAsync(0x0001, new MyData { score = 42 });
```

### 7 — Disconnect

```csharp
client.Disconnect(); // sends close frame; auto-reconnect is disabled permanently
```

---

## SyncVars

Server-authoritative state that updates automatically. Mirror the server's
`syncvar.SyncVar` (payloads are UTF-8 — e.g. `strconv.FormatFloat`).

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

Each `[SyncVar]` field is assigned from the incoming payload whenever its
command ID arrives. `SyncVarType` supports `Int32`, `Int64`, `Float32`,
`Float64`, `Bool`, `String`.



## JSON-RPC

The server supports JSON-RPC 2.0 for request-response style calls.

```csharp
// paramsJson must be a pre-serialised JSON value.
// Returns the full raw JSON-RPC response string on success.
string raw = await client.SendJsonRpcAsync(
    method:     "getScore",
    paramsJson: "{\"userId\": 42}");

// Parse the "result" field however you like:
var resp = JsonUtility.FromJson<MyResponse>(raw);

// Or use the generic overload — parses "result" for you:
var score = await client.SendJsonRpcAsync<ScoreResult>(
    method:     "getScore",
    paramsJson: "{\"userId\": 42}");
```

`SendJsonRpcAsync` throws:
- `TimeoutException` — no response within 30 seconds.
- `Exception` — server returned a JSON-RPC error object.
- `InvalidOperationException` — client is not connected.

---

## JSON Serialisation Notes

`Client` has zero external dependencies and uses Unity's built-in
`JsonUtility` for the narrow set of JSON it needs internally.

For **sending** complex types (dictionaries, anonymous objects, nested lists)
use [Newtonsoft.Json for Unity](https://docs.unity3d.com/Manual/com.unity.nuget.newtonsoft-json.html)
(`com.unity.nuget.newtonsoft-json`) and call `SendStringAsync` directly:

```csharp
// Install via Package Manager → "com.unity.nuget.newtonsoft-json"
using Newtonsoft.Json;

var data = new Dictionary<string, object> { ["key"] = "value" };
await client.SendStringAsync(0x0001, JsonConvert.SerializeObject(data));

// JSON-RPC with complex params:
string paramsJson = JsonConvert.SerializeObject(new { userId = 42, room = "lobby" });
string raw = await client.SendJsonRpcAsync("joinRoom", paramsJson);
```

---

## Protocol Reference

### Wire format

```
┌────────────────┬──────────────────────────────────────┐
│  CommandID     │  Payload                             │
│  (4 bytes BE)  │  (0 … 10 MiB)                       │
└────────────────┴──────────────────────────────────────┘
```

### Reserved command IDs

| Constant | Value | Purpose |
|---|---|---|
| `ReservedCommands.JsonRpc` | `0xFFFFFFFF` | JSON-RPC 2.0 envelope |
| `ReservedCommands.JsonRpcError` | `0xFFFFFFFE` | JSON-RPC error response |
| `ReservedCommands.InvalidCommand` | `0xFFFFFFFD` | Unknown command (server → client) |
| `ReservedCommands.CommandError` | `0xFFFFFFFC` | Command processing error |

User-defined command IDs must be **below `0xFFFFFFFC`**.

### Reconnect backoff

```
delay = reconnectDelay × min(attempt, 5)
```

| Attempt | Delay (reconnectDelay = 3 s) |
|---|---|
| 1 | 3 s |
| 2 | 6 s |
| 3 | 9 s |
| 4 | 12 s |
| 5+ | 15 s |

---

## Full Example

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

        _client.OnConnected    += HandleConnected;
        _client.OnDisconnected += HandleDisconnected;
        _client.OnError        += HandleError;

        // Register handlers before connecting.
        _client.On(0x0001, OnChatMessage);
        _client.On(0x0002, OnGameState);

        try
        {
            await _client.ConnectAsync();
            // Connection is open here.
            await _client.SendStringAsync(0x0001, "Hello from Unity!");
        }
        catch (Exception ex)
        {
            Debug.LogError($"Could not connect: {ex.Message}");
        }
    }

    private void OnDestroy() => _client?.Disconnect();

    // --- Event handlers (main thread) ---

    private void HandleConnected()
    {
        Debug.Log("Connected to server.");
    }

    private void HandleDisconnected(int code, string reason)
    {
        Debug.Log($"Disconnected: {code} {reason}");
    }

    private void HandleError(string msg)
    {
        Debug.LogError($"Network error: {msg}");
    }

    // --- Command handlers (main thread) ---

    private void OnChatMessage(byte[] payload)
    {
        string text = System.Text.Encoding.UTF8.GetString(payload);
        Debug.Log($"Chat: {text}");
    }

    private void OnGameState(byte[] payload)
    {
        // Deserialize with JsonUtility or Newtonsoft.
        // var state = JsonUtility.FromJson<GameState>(
        //     System.Text.Encoding.UTF8.GetString(payload));
    }

    // --- JSON-RPC call example ---

    public async Task<string> GetLeaderboard(int limit)
    {
        string paramsJson = $"{{\"limit\":{limit}}}";
        return await _client.SendJsonRpcAsync("getLeaderboard", paramsJson);
    }
}
```
