using System;
using System.Collections.Concurrent;
using System.Collections.Generic;
using System.Net.WebSockets;
using System.Text;
using System.Threading;
using System.Threading.Tasks;
using UnityEngine;

namespace Knet
{
    /// <summary>
    /// Configuration for <see cref="KNetClient"/>.
    /// Fields are exposed in the Unity Inspector when the client is added as a component.
    /// </summary>
    [Serializable]
    public class KNetClientConfig
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
    ///   var client = gameObject.AddComponent&lt;KNetClient&gt;();
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
    public class KNetClient : MonoBehaviour
    {
        // -------------------------------------------------------------------------
        // Serialised configuration
        // -------------------------------------------------------------------------

        [SerializeField]
        private KNetClientConfig config = new KNetClientConfig();

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
        private KNetConnectionState _state = KNetConnectionState.Disconnected;
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
        public KNetClientConfig Config => config;

        /// <summary>Current connection state.</summary>
        public KNetConnectionState State
        {
            get { lock (_stateLock) { return _state; } }
        }

        /// <summary>
        /// <c>true</c> when the WebSocket handshake has completed and the channel
        /// is open for sending and receiving messages.
        /// </summary>
        public bool IsConnected =>
            State == KNetConnectionState.Connected &&
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
        /// Opens a WebSocket connection to <see cref="KNetClientConfig.url"/>.
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
                if (_state == KNetConnectionState.Connected ||
                    _state == KNetConnectionState.Connecting)
                {
                    Log("Already connected or connecting.");
                    return;
                }
                _state = KNetConnectionState.Connecting;
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
                    _state = KNetConnectionState.Connected;
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
            lock (_stateLock) { _state = KNetConnectionState.Closed; }
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
        /// <para>Command IDs &gt;= <see cref="KNetReservedCommands.CommandError"/>
        /// are reserved by the protocol and cannot be registered here.</para>
        /// </summary>
        /// <param name="commandId">The command to listen for.</param>
        /// <param name="handler">
        /// Callback that receives the raw payload bytes.
        /// Invoked on the Unity main thread — safe to call Unity APIs from here.
        /// </param>
        public void On(uint commandId, Action<byte[]> handler)
        {
            if (commandId >= KNetReservedCommands.CommandError)
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

        // -------------------------------------------------------------------------
        // Sending
        // -------------------------------------------------------------------------

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
            var encoded = KNetProtocol.Encode(commandId, payload);

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
                await SendStringAsync(KNetReservedCommands.JsonRpc, requestJson, ct)
                    .ConfigureAwait(false);
            }
            catch (Exception ex)
            {
                lock (_jsonRpcLock) { _jsonRpcPending.Remove(id); }
                tcs.TrySetException(ex);
            }

            return await tcs.Task.ConfigureAwait(false);
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
                var (commandId, payload) = KNetProtocol.Decode(data);
                Log($"Received 0x{commandId:X8} ({payload.Length} bytes).");

                if (commandId == KNetReservedCommands.JsonRpc ||
                    commandId == KNetReservedCommands.JsonRpcError)
                {
                    HandleJsonRpcResponse(
                        payload,
                        isError: commandId == KNetReservedCommands.JsonRpcError);
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
                    _state != KNetConnectionState.Closed &&
                    (config.maxReconnectAttempts == 0 ||
                     _reconnectAttempts < config.maxReconnectAttempts);

                if (_state != KNetConnectionState.Closed)
                    _state = KNetConnectionState.Disconnected;
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
                    _state != KNetConnectionState.Closed &&
                    (config.maxReconnectAttempts == 0 ||
                     _reconnectAttempts < config.maxReconnectAttempts);

                if (_state != KNetConnectionState.Closed)
                    _state = KNetConnectionState.Disconnected;
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
                if (_state != KNetConnectionState.Closed)
                    _state = KNetConnectionState.Reconnecting;
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
                Debug.Log($"[KNetClient] {message}");
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
