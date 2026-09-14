using System;
using System.Collections.Concurrent;
using System.Collections.Generic;
using System.Net.WebSockets;
using System.Reflection;
using System.Text;
using System.Threading;
using System.Threading.Tasks;
using UnityEngine;

namespace Knet
{
    /// <summary>
    /// Configuration for <see cref="Client"/>.
    /// Fields are exposed in the Unity Inspector when the client is added as a component.
    /// </summary>
    [Serializable]
    public class ClientConfig
    {
        [Tooltip("WebSocket server URL. Use ws:// for plain or wss:// for TLS.")]
        public string url = "ws://localhost:8080/ws";

        [Tooltip("Automatically reconnect when the connection drops unexpectedly.")]
        public bool autoReconnect = true;

        [Tooltip("Base delay in seconds between reconnect attempts. " +
                 "Actual delay = reconnectDelay × min(attempt, 5).")]
        public float reconnectDelay = 3f;

        [Tooltip("Maximum reconnect attempts. 0 = unlimited.")]
        public int maxReconnectAttempts = 0;

        [Tooltip("Seconds to wait for the initial WebSocket handshake before timing out.")]
        public float connectionTimeout = 10f;

        [Tooltip("Enable verbose debug logging.")]
        public bool debug = false;
    }

    /// <summary>
    /// Unity WebSocket client for the knet protocol.
    ///
    /// <para>Wire format (mirrors the Go server):</para>
    /// <code>
    ///   [0..3]  CommandID — uint32, big-endian
    ///   [4..]   Payload   — arbitrary bytes
    /// </code>
    ///
    /// <para><b>Quick start:</b></para>
    /// <code>
    ///   var client = gameObject.AddComponent&lt;Client&gt;();
    ///   client.Config.url   = "ws://localhost:8080/ws";
    ///   client.Config.debug = true;
    ///
    ///   client.OnConnected    += () => Debug.Log("Connected!");
    ///   client.OnDisconnected += (code, reason) => Debug.Log($"Closed: {code}");
    ///
    ///   client.On(0x0001, payload => {
    ///       Debug.Log("Received: " + Encoding.UTF8.GetString(payload));
    ///   });
    ///
    ///   await client.ConnectAsync();
    ///   await client.SendStringAsync(0x0001, "Hello server!");
    /// </code>
    ///
    /// <para>
    /// Callbacks registered with <see cref="On"/> and all events are always invoked
    /// on the <b>Unity main thread</b>, so they are safe to use with Unity APIs.
    /// </para>
    /// </summary>
    public class Client : MonoBehaviour
    {
        // -------------------------------------------------------------------------
        // Serialised configuration
        // -------------------------------------------------------------------------

        [SerializeField]
        private ClientConfig config = new ClientConfig();

        // -------------------------------------------------------------------------
        // Public events (invoked on the Unity main thread)
        // -------------------------------------------------------------------------

        /// <summary>Raised when the WebSocket connection is successfully opened.</summary>
        public event Action OnConnected;

        /// <summary>
        /// Raised when the connection is closed.
        /// Parameters: WebSocket close code, close reason string.
        /// </summary>
        public event Action<int, string> OnDisconnected;

        /// <summary>Raised when a connection or send error occurs.</summary>
        public event Action<string> OnError;

        // -------------------------------------------------------------------------
        // Private state
        // -------------------------------------------------------------------------

        private ClientWebSocket _ws;
        private ConnectionState _state = ConnectionState.Disconnected;
        private readonly object _stateLock = new object();

        // Handlers registered with On(); accessed from both main thread and receive thread.
        private readonly ConcurrentDictionary<uint, Action<byte[]>> _handlers =
            new ConcurrentDictionary<uint, Action<byte[]>>();

        // Pending JSON-RPC requests keyed by request-ID string.
        private readonly Dictionary<string, TaskCompletionSource<string>> _jsonRpcPending =
            new Dictionary<string, TaskCompletionSource<string>>();
        private readonly object _jsonRpcLock = new object();

        // Actions produced on background threads and drained on the main thread in Update().
        private readonly ConcurrentQueue<Action> _mainThreadQueue = new ConcurrentQueue<Action>();

        // Ensures at most one concurrent WebSocket send (ClientWebSocket requirement).
        private readonly SemaphoreSlim _sendSemaphore = new SemaphoreSlim(1, 1);

        private CancellationTokenSource _cts;
        private int _reconnectAttempts;
        private float _reconnectTimer = -1f; // seconds; -1 = not scheduled

        // Auto-incrementing counter for JSON-RPC request IDs.
        private long _rpcIdCounter;

        // -------------------------------------------------------------------------
        // Public properties
        // -------------------------------------------------------------------------

        /// <summary>
        /// Direct access to the configuration object.
        /// Modify fields before calling <see cref="ConnectAsync"/>.
        /// </summary>
        public ClientConfig Config => config;

        /// <summary>Current connection state.</summary>
        public ConnectionState State
        {
            get { lock (_stateLock) { return _state; } }
        }

        /// <summary>
        /// <c>true</c> when the WebSocket handshake has completed and the channel
        /// is open for sending and receiving messages.
        /// </summary>
        public bool IsConnected =>
            State == ConnectionState.Connected &&
            _ws != null &&
            _ws.State == WebSocketState.Open;

        // -------------------------------------------------------------------------
        // MonoBehaviour lifecycle
        // -------------------------------------------------------------------------

        private void Update()
        {
            // Drain callbacks queued from background threads.
            while (_mainThreadQueue.TryDequeue(out var action))
            {
                try { action(); }
                catch (Exception ex)
                {
                    Log($"Main-thread callback error: {ex.Message}");
                }
            }

            // Reconnect countdown timer (ticked only on the main thread).
            if (_reconnectTimer > 0f)
            {
                _reconnectTimer -= Time.unscaledDeltaTime;
                if (_reconnectTimer <= 0f)
                {
                    _reconnectTimer = -1f;
                    _ = ConnectAsync(); // fire-and-forget
                }
            }
        }

        private void OnDestroy()
        {
            ForceClose();
            _cts?.Dispose();
            _sendSemaphore.Dispose();
        }

        // -------------------------------------------------------------------------
        // Connection management
        // -------------------------------------------------------------------------

        /// <summary>
        /// Opens a WebSocket connection to <see cref="ClientConfig.url"/>.
        ///
        /// <para>Returns once the connection handshake succeeds.
        /// Throws if the connection cannot be established (e.g., timeout, refused).</para>
        ///
        /// <para>The receive loop runs in the background for the lifetime of the
        /// connection. Reconnection (if configured) is also handled automatically.</para>
        /// </summary>
        public async Task ConnectAsync()
        {
            lock (_stateLock)
            {
                if (_state == ConnectionState.Connected ||
                    _state == ConnectionState.Connecting)
                {
                    Log("Already connected or connecting.");
                    return;
                }
                _state = ConnectionState.Connecting;
            }

            Log($"Connecting to {config.url}...");

            // Replace the cancellation token source for this session.
            var oldCts = _cts;
            _cts = new CancellationTokenSource();
            oldCts?.Cancel();
            oldCts?.Dispose();

            _ws?.Dispose();
            _ws = new ClientWebSocket();

            try
            {
                using var timeoutCts =
                    CancellationTokenSource.CreateLinkedTokenSource(_cts.Token);
                timeoutCts.CancelAfter(TimeSpan.FromSeconds(config.connectionTimeout));

                await _ws.ConnectAsync(new Uri(config.url), timeoutCts.Token)
                          .ConfigureAwait(false);

                lock (_stateLock)
                {
                    _state = ConnectionState.Connected;
                    _reconnectAttempts = 0;
                }

                Log("Connected.");
                EnqueueMain(() => OnConnected?.Invoke());

                // Run the receive loop in the background; exceptions are caught inside it.
                _ = ReceiveLoopAsync(_cts.Token);
            }
            catch (OperationCanceledException) when (!_cts.IsCancellationRequested)
            {
                HandleConnectionError("Connection timeout.");
            }
            catch (OperationCanceledException)
            {
                Log("Connection attempt cancelled.");
            }
            catch (Exception ex)
            {
                HandleConnectionError(ex.Message);
            }
        }

        /// <summary>
        /// Closes the WebSocket connection with a normal close code and permanently
        /// disables auto-reconnect.
        ///
        /// <para>Safe to call from any thread.</para>
        /// </summary>
        public void Disconnect()
        {
            Log("Disconnecting...");
            ForceClose();
        }

        private void ForceClose()
        {
            lock (_stateLock) { _state = ConnectionState.Closed; }
            _reconnectTimer = -1f;
            _cts?.Cancel();

            if (_ws != null &&
                (_ws.State == WebSocketState.Open ||
                 _ws.State == WebSocketState.CloseReceived))
            {
                try
                {
                    _ = _ws.CloseAsync(
                        WebSocketCloseStatus.NormalClosure,
                        "Client disconnect",
                        CancellationToken.None);
                }
                catch { /* ignore errors during explicit disconnect */ }
            }
        }

        // -------------------------------------------------------------------------
        // Handler registration
        // -------------------------------------------------------------------------

        /// <summary>
        /// Registers a callback that is invoked (on the Unity main thread) whenever
        /// a message with <paramref name="commandId"/> is received.
        ///
        /// <para>Command IDs &gt;= <see cref="ReservedCommands.CommandError"/>
        /// are reserved by the protocol and cannot be registered here.</para>
        /// </summary>
        /// <param name="commandId">The command to listen for.</param>
        /// <param name="handler">
        /// Callback that receives the raw payload bytes.
        /// Invoked on the Unity main thread — safe to call Unity APIs from here.
        /// </param>
        public void On(uint commandId, Action<byte[]> handler)
        {
            if (commandId >= ReservedCommands.CommandError)
                throw new ArgumentException(
                    $"Command ID 0x{commandId:X8} is reserved by the knet protocol.",
                    nameof(commandId));

            _handlers[commandId] = handler;
            Log($"Registered handler for command 0x{commandId:X8}.");
        }

        /// <summary>Removes the handler registered for <paramref name="commandId"/>.</summary>
        public void Off(uint commandId)
        {
            _handlers.TryRemove(commandId, out _);
            Log($"Unregistered handler for command 0x{commandId:X8}.");
        }

        /// <summary>
        /// Binds all fields marked with <see cref="SyncVarAttribute"/> on
        /// <paramref name="target"/> to incoming commands.
        ///
        /// <para>
        /// Call once (e.g. in <c>Start()</c>). Each marked field is assigned from
        /// the decoded payload whenever its command ID arrives.
        /// </para>
        /// </summary>
        /// <param name="target">A MonoBehaviour (or plain object) with trailing
        /// <c>[SyncVar]</c> fields.</param>
        public void BindSyncVars(object target)
        {
            foreach (var field in target.GetType().GetFields(BindingFlags.Instance | BindingFlags.Public | BindingFlags.NonPublic))
            {
                var attr = field.GetCustomAttribute<SyncVarAttribute>();
                if (attr == null) continue;

                On(attr.CommandId, payload =>
                {
                    try
                    {
                        field.SetValue(target, DecodeSyncVar(attr.Type, payload));
                    }
                    catch (Exception ex)
                    {
                        Log($"SyncVar 0x{attr.CommandId:X8} decode failed: {ex.Message}");
                    }
                });

                Log($"Bound SyncVar field '{field.Name}' to command 0x{attr.CommandId:X8}.");
            }
        }

        private static object DecodeSyncVar(SyncVarType declared, byte[] payload)
        {
            if (payload.Length < 1)
                throw new ArgumentException("sync var payload is empty.");

            if ((byte)declared != payload[0])
                throw new InvalidOperationException(
                    $"sync var type mismatch: payload tag 0x{payload[0]:X2} does not match declared {declared}.");

            var s = Encoding.UTF8.GetString(payload, 1, payload.Length - 1);
            switch (declared)
            {
                case SyncVarType.Int32: return int.Parse(s);
                case SyncVarType.Int64: return long.Parse(s);
                case SyncVarType.Float32: return float.Parse(s, System.Globalization.CultureInfo.InvariantCulture);
                case SyncVarType.Float64: return double.Parse(s, System.Globalization.CultureInfo.InvariantCulture);
                case SyncVarType.Bool: return bool.Parse(s);
                case SyncVarType.String: return s;
                default: throw new ArgumentOutOfRangeException(nameof(declared), declared, null);
            }
        }

        // -------------------------------------------------------------------------
        // Sending
        // -------------------------------------------------------------------------

        /// <summary>
        /// Encodes and sends a sync var value as a tagged payload matching the
        /// server's <c>syncvar</c> wire format (<c>[1-byte tag][value]</c>).
        /// </summary>
        /// <typeparam name="T">CLR type; only the six <see cref="SyncVarType"/>
        /// categories are wire-compatible.</typeparam>
        /// <param name="commandId">Command identifier.</param>
        /// <param name="value">Value to send.</param>
        /// <param name="ct">Optional cancellation token.</param>
        public Task SendSyncVarAsync<T>(uint commandId, T value, CancellationToken ct = default)
        {
            var (type, text) = EncodeSyncVar(value);
            var body = Encoding.UTF8.GetBytes(text);
            var payload = new byte[1 + body.Length];
            payload[0] = (byte)type;
            Buffer.BlockCopy(body, 0, payload, 1, body.Length);
            return SendAsync(commandId, payload, ct);
        }

        private static (SyncVarType type, string text) EncodeSyncVar<T>(T value)
        {
            switch (value)
            {
                case int i: return (SyncVarType.Int32, i.ToString(System.Globalization.CultureInfo.InvariantCulture));
                case long l: return (SyncVarType.Int64, l.ToString(System.Globalization.CultureInfo.InvariantCulture));
                case float f: return (SyncVarType.Float32, f.ToString(System.Globalization.CultureInfo.InvariantCulture));
                case double d: return (SyncVarType.Float64, d.ToString(System.Globalization.CultureInfo.InvariantCulture));
                case bool b: return (SyncVarType.Bool, b.ToString().ToLowerInvariant());
                case string s: return (SyncVarType.String, s);
                default: throw new ArgumentException(
                    $"sync var: unsupported type {typeof(T).Name}; wire supports Int32/Int64/Float32/Float64/Bool/String.",
                    nameof(value));
            }
        }

        /// <summary>
        /// Encodes and sends a binary command to the server.
        /// </summary>
        /// <param name="commandId">The command identifier.</param>
        /// <param name="payload">
        /// Raw payload bytes. Pass <c>null</c> or an empty array for a header-only message.
        /// </param>
        /// <param name="ct">Optional cancellation token.</param>
        /// <exception cref="InvalidOperationException">Thrown when not connected.</exception>
        public async Task SendAsync(uint commandId, byte[] payload, CancellationToken ct = default)
        {
            if (!IsConnected)
                throw new InvalidOperationException("Not connected to server.");

            payload ??= Array.Empty<byte>();
            var encoded = Protocol.Encode(commandId, payload);

            await _sendSemaphore.WaitAsync(ct).ConfigureAwait(false);
            try
            {
                await _ws.SendAsync(
                    new ArraySegment<byte>(encoded),
                    WebSocketMessageType.Binary,
                    endOfMessage: true,
                    cancellationToken: ct).ConfigureAwait(false);

                Log($"Sent 0x{commandId:X8} ({payload.Length} byte payload).");
            }
            finally
            {
                _sendSemaphore.Release();
            }
        }

        /// <summary>
        /// Encodes <paramref name="text"/> as UTF-8 and sends it as the payload.
        /// </summary>
        public Task SendStringAsync(uint commandId, string text, CancellationToken ct = default)
            => SendAsync(commandId, Encoding.UTF8.GetBytes(text ?? string.Empty), ct);

        /// <summary>
        /// Serialises <paramref name="data"/> to JSON via Unity's <c>JsonUtility</c>
        /// and sends the result as a UTF-8 payload.
        ///
        /// <para>
        /// <c>JsonUtility</c> only supports plain serialisable classes/structs
        /// (no dictionaries, no anonymous objects). For richer types use
        /// <c>Newtonsoft.Json.JsonConvert.SerializeObject</c> and call
        /// <see cref="SendStringAsync"/> directly.
        /// </para>
        /// </summary>
        public Task SendJsonAsync<T>(uint commandId, T data, CancellationToken ct = default)
            => SendStringAsync(commandId, JsonUtility.ToJson(data), ct);

        /// <summary>
        /// Sends a JSON-RPC 2.0 request and awaits the server response.
        ///
        /// <para>
        /// <paramref name="paramsJson"/> must be a pre-serialised JSON value
        /// (object, array, <c>"null"</c>, etc.). Omit or pass <c>null</c> for
        /// a request with no params.
        /// </para>
        ///
        /// <para>
        /// On success the returned string is the complete JSON-RPC response object.
        /// Parse the <c>"result"</c> field yourself with JsonUtility or Newtonsoft.
        /// Throws <see cref="TimeoutException"/> after 30 seconds,
        /// or <see cref="Exception"/> when the server returns an error object.
        /// </para>
        ///
        /// <example>
        /// <code>
        /// string raw = await client.SendJsonRpcAsync("getScore", "{\"userId\":42}");
        /// var resp = JsonUtility.FromJson&lt;MyResponse&gt;(raw);
        /// </code>
        /// </example>
        /// </summary>
        public async Task<string> SendJsonRpcAsync(
            string method,
            string paramsJson = null,
            CancellationToken ct = default)
        {
            if (!IsConnected)
                throw new InvalidOperationException("Not connected to server.");

            var id = Interlocked.Increment(ref _rpcIdCounter).ToString();
            var tcs = new TaskCompletionSource<string>(
                TaskCreationOptions.RunContinuationsAsynchronously);

            lock (_jsonRpcLock) { _jsonRpcPending[id] = tcs; }

            using var timeoutCts = CancellationTokenSource.CreateLinkedTokenSource(ct);
            timeoutCts.CancelAfter(TimeSpan.FromSeconds(30));
            timeoutCts.Token.Register(() =>
            {
                lock (_jsonRpcLock) { _jsonRpcPending.Remove(id); }
                tcs.TrySetException(new TimeoutException("JSON-RPC request timed out."));
            });

            var safeParams = string.IsNullOrEmpty(paramsJson) ? "null" : paramsJson;
            var requestJson =
                $"{{\"jsonrpc\":\"2.0\"," +
                $"\"method\":\"{EscapeJson(method)}\"," +
                $"\"id\":\"{id}\"," +
                $"\"params\":{safeParams}}}";

            try
            {
                await SendStringAsync(ReservedCommands.JsonRpc, requestJson, ct)
                    .ConfigureAwait(false);
            }
            catch (Exception ex)
            {
                lock (_jsonRpcLock) { _jsonRpcPending.Remove(id); }
                tcs.TrySetException(ex);
            }

            return await tcs.Task.ConfigureAwait(false);
        }

        /// <summary>
        /// Sends a JSON-RPC 2.0 request and returns the deserialised <c>"result"</c> field.
        ///
        /// <para>
        /// Convenience wrapper over <see cref="SendJsonRpcAsync(string,string,CancellationToken)"/>
        /// that skips manual envelope parsing. Uses Unity's <c>JsonUtility</c>, so
        /// <typeparamref name="TResult"/> must be a plain serialisable class/struct
        /// (no dictionaries/arrays at the top level). For richer result shapes, call
        /// the string-returning overload and parse with Newtonsoft yourself.
        /// </para>
        ///
        /// <example>
        /// <code>
        /// var score = await client.SendJsonRpcAsync&lt;ScoreResult&gt;("getScore", "{\"userId\":42}");
        /// </code>
        /// </example>
        /// </summary>
        public async Task<TResult> SendJsonRpcAsync<TResult>(
            string method,
            string paramsJson = null,
            CancellationToken ct = default)
        {
            var raw = await SendJsonRpcAsync(method, paramsJson, ct).ConfigureAwait(false);
            return JsonUtility.FromJson<JsonRpcResultEnvelope<TResult>>(raw).result;
        }

        // -------------------------------------------------------------------------
        // Background receive loop
        // -------------------------------------------------------------------------

        private async Task ReceiveLoopAsync(CancellationToken ct)
        {
            var buffer = new byte[8192];
            var fragments = new List<(byte[] data, int count)>();

            try
            {
                while (_ws.State == WebSocketState.Open && !ct.IsCancellationRequested)
                {
                    fragments.Clear();
                    int totalBytes = 0;
                    WebSocketReceiveResult result;

                    do
                    {
                        result = await _ws.ReceiveAsync(new ArraySegment<byte>(buffer), ct)
                                          .ConfigureAwait(false);

                        if (result.MessageType == WebSocketMessageType.Close)
                        {
                            var code = (int)(result.CloseStatus ?? WebSocketCloseStatus.NormalClosure);
                            HandleClose(code, result.CloseStatusDescription ?? string.Empty);
                            return;
                        }

                        if (result.Count > 0)
                        {
                            var fragment = new byte[result.Count];
                            Buffer.BlockCopy(buffer, 0, fragment, 0, result.Count);
                            fragments.Add((fragment, result.Count));
                            totalBytes += result.Count;
                        }
                    }
                    while (!result.EndOfMessage);

                    // Assemble the complete message from fragments.
                    var message = new byte[totalBytes];
                    int offset = 0;
                    foreach (var (frag, count) in fragments)
                    {
                        Buffer.BlockCopy(frag, 0, message, offset, count);
                        offset += count;
                    }

                    HandleMessage(message);
                }
            }
            catch (OperationCanceledException) { /* normal: Disconnect() or context cancellation */ }
            catch (WebSocketException ex) when (!ct.IsCancellationRequested)
            {
                HandleClose(1006, ex.Message);
            }
            catch (Exception ex) when (!ct.IsCancellationRequested)
            {
                Log($"Receive loop error: {ex.Message}");
                HandleClose(1011, ex.Message);
            }
        }

        // -------------------------------------------------------------------------
        // Message dispatch
        // -------------------------------------------------------------------------

        private void HandleMessage(byte[] data)
        {
            try
            {
                var (commandId, payload) = Protocol.Decode(data);
                Log($"Received 0x{commandId:X8} ({payload.Length} bytes).");

                if (commandId == ReservedCommands.JsonRpc ||
                    commandId == ReservedCommands.JsonRpcError)
                {
                    HandleJsonRpcResponse(
                        payload,
                        isError: commandId == ReservedCommands.JsonRpcError);
                    return;
                }

                if (_handlers.TryGetValue(commandId, out var handler))
                {
                    EnqueueMain(() =>
                    {
                        try { handler(payload); }
                        catch (Exception ex)
                        {
                            Log($"Handler error for 0x{commandId:X8}: {ex.Message}");
                        }
                    });
                }
                else
                {
                    Log($"No handler registered for command 0x{commandId:X8}.");
                }
            }
            catch (Exception ex)
            {
                Log($"Failed to decode message: {ex.Message}");
            }
        }

        private void HandleJsonRpcResponse(byte[] payload, bool isError)
        {
            try
            {
                var json = Encoding.UTF8.GetString(payload);
                var envelope = JsonUtility.FromJson<JsonRpcResponseEnvelope>(json);
                if (envelope == null) return;

                TaskCompletionSource<string> tcs;
                lock (_jsonRpcLock)
                {
                    if (!_jsonRpcPending.TryGetValue(envelope.id, out tcs)) return;
                    _jsonRpcPending.Remove(envelope.id);
                }

                if (isError || !string.IsNullOrEmpty(envelope.error?.message))
                    tcs.TrySetException(new Exception(envelope.error?.message ?? "JSON-RPC error"));
                else
                    tcs.TrySetResult(json);
            }
            catch (Exception ex)
            {
                Log($"Failed to handle JSON-RPC response: {ex.Message}");
            }
        }

        // -------------------------------------------------------------------------
        // Connection state transitions
        // -------------------------------------------------------------------------

        private void HandleClose(int code, string reason)
        {
            var display = string.IsNullOrEmpty(reason) ? "none" : reason;
            Log($"Connection closed (code: {code}, reason: {display}).");

            bool shouldReconnect;
            lock (_stateLock)
            {
                shouldReconnect =
                    config.autoReconnect &&
                    _state != ConnectionState.Closed &&
                    (config.maxReconnectAttempts == 0 ||
                     _reconnectAttempts < config.maxReconnectAttempts);

                if (_state != ConnectionState.Closed)
                    _state = ConnectionState.Disconnected;
            }

            FailAllPendingRpc("Connection closed.");
            EnqueueMain(() => OnDisconnected?.Invoke(code, reason));

            if (shouldReconnect)
                ScheduleReconnect();
        }

        private void HandleConnectionError(string message)
        {
            Log($"Connection error: {message}");

            bool shouldReconnect;
            lock (_stateLock)
            {
                shouldReconnect =
                    config.autoReconnect &&
                    _state != ConnectionState.Closed &&
                    (config.maxReconnectAttempts == 0 ||
                     _reconnectAttempts < config.maxReconnectAttempts);

                if (_state != ConnectionState.Closed)
                    _state = ConnectionState.Disconnected;
            }

            FailAllPendingRpc(message);
            EnqueueMain(() => OnError?.Invoke(message));

            if (shouldReconnect)
                ScheduleReconnect();
        }

        private void ScheduleReconnect()
        {
            if (_reconnectTimer > 0f) return;

            _reconnectAttempts++;
            lock (_stateLock)
            {
                if (_state != ConnectionState.Closed)
                    _state = ConnectionState.Reconnecting;
            }

            // Backoff: delay × min(attempt, 5) — mirrors the JS client behaviour.
            float delay = config.reconnectDelay * Mathf.Min(_reconnectAttempts, 5);
            Log($"Reconnecting in {delay:F1}s (attempt {_reconnectAttempts})...");

            EnqueueMain(() => { _reconnectTimer = delay; });
        }

        private void FailAllPendingRpc(string reason)
        {
            List<TaskCompletionSource<string>> pending;
            lock (_jsonRpcLock)
            {
                pending = new List<TaskCompletionSource<string>>(_jsonRpcPending.Values);
                _jsonRpcPending.Clear();
            }
            foreach (var tcs in pending)
                tcs.TrySetException(new Exception(reason));
        }

        // -------------------------------------------------------------------------
        // Utilities
        // -------------------------------------------------------------------------

        private void EnqueueMain(Action action) => _mainThreadQueue.Enqueue(action);

        private void Log(string message)
        {
            if (config.debug)
                Debug.Log($"[Client] {message}");
        }

        private static string EscapeJson(string s)
        {
            if (string.IsNullOrEmpty(s)) return string.Empty;
            return s.Replace("\\", "\\\\")
                    .Replace("\"", "\\\"")
                    .Replace("\b", "\\b")
                    .Replace("\f", "\\f")
                    .Replace("\n", "\\n")
                    .Replace("\r", "\\r")
                    .Replace("\t", "\\t");
        }

        // -------------------------------------------------------------------------
        // Private serialisation helpers (JsonUtility-compatible)
        // -------------------------------------------------------------------------

        [Serializable]
        private class JsonRpcResponseEnvelope
        {
#pragma warning disable 0649
            // ReSharper disable InconsistentNaming
            public string jsonrpc;
            public string id;
            public JsonRpcErrorEnvelope error;
            // ReSharper restore InconsistentNaming
#pragma warning restore 0649
        }

        [Serializable]
        private class JsonRpcResultEnvelope<TResult>
        {
#pragma warning disable 0649
            public TResult result;
#pragma warning restore 0649
        }

        [Serializable]
        private class JsonRpcErrorEnvelope
        {
#pragma warning disable 0649
            // ReSharper disable InconsistentNaming
            public int code;
            public string message;
            // ReSharper restore InconsistentNaming
#pragma warning restore 0649
        }
    }
}
