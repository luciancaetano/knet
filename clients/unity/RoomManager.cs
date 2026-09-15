using System;
using System.Collections.Generic;
using System.Text;
using System.Threading.Tasks;
using UnityEngine;

namespace Knet
{
    /// <summary>Result of a successful <see cref="RoomManager.JoinRoomAsync"/> call.</summary>
    public class RoomJoinResult
    {
        public string RoomId;
        public string[] Members;
    }

    /// <summary>
    /// Optional room join/leave/membership layer on top of <see cref="Client"/>,
    /// wrapping the wire protocol defined in <c>roommanager/protocol.go</c>
    /// (Go server side). Mirrors the TS client's <c>RoomManager</c>
    /// (<c>clients/js/src/room-manager.ts</c>).
    ///
    /// <para>
    /// Reconnect caveat: <see cref="Client"/> does not currently persist/resend a
    /// session ID across reconnects, so the server can never treat a reconnect as
    /// a resume (see <c>spec/spec-architecture-room-manager.md</c> CON-005). Every
    /// reconnect is therefore treated as a fresh connection: <see cref="CurrentRooms"/>
    /// is cleared and <see cref="OnRoomsLost"/> fires. The <c>ResumeSync</c> handler
    /// is wired for forward-compatibility but is unreachable with the client as it
    /// exists today.
    /// </para>
    /// </summary>
    public class RoomManager
    {
        private const float DefaultRequestTimeoutSeconds = 10f;

        private readonly Client _client;
        private readonly float _timeoutSeconds;

        private readonly List<string> _rooms = new List<string>();
        private bool _hasConnectedOnce;

        private readonly Dictionary<string, TaskCompletionSource<RoomJoinResult>> _pendingJoins =
            new Dictionary<string, TaskCompletionSource<RoomJoinResult>>();
        private readonly Dictionary<string, TaskCompletionSource<bool>> _pendingLeaves =
            new Dictionary<string, TaskCompletionSource<bool>>();
        private readonly object _pendingLock = new object();

        /// <summary>Raised (main thread) when another client joins a room we're in.</summary>
        public event Action<string, string> OnMemberJoined;

        /// <summary>Raised (main thread) when another client leaves a room we're in.</summary>
        public event Action<string, string> OnMemberLeft;

        /// <summary>Raised (main thread) when another member disconnects (grace period pending).</summary>
        public event Action<string, string> OnMemberDisconnected;

        /// <summary>Raised (main thread) when a previously-disconnected member reconnects.</summary>
        public event Action<string, string> OnMemberReconnected;

        /// <summary>
        /// Raised (main thread) when a reconnect invalidates our room membership
        /// (see class doc / CON-005) — <see cref="CurrentRooms"/> is already empty by then.
        /// </summary>
        public event Action OnRoomsLost;

        /// <summary>Raised (main thread) when another room member sends a message via <see cref="SendMessageAsync"/>.</summary>
        public event Action<string, string, string, string> OnMessage;

        private RoomManager(Client client, float timeoutSeconds)
        {
            _client = client;
            _timeoutSeconds = timeoutSeconds;

            client.On(RoomCommands.JoinAck, payload => HandleJoinAck(Decode<RoomJoinAck>(payload)));
            client.On(RoomCommands.LeaveAck, payload => HandleLeaveAck(Decode<RoomLeaveAck>(payload)));
            client.On(RoomCommands.Error, payload => HandleError(Decode<RoomError>(payload)));
            client.On(RoomCommands.MemberEvent, payload => HandleMemberEvent(Decode<RoomMemberEvent>(payload)));
            client.On(RoomCommands.ResumeSync, payload => HandleResumeSync(Decode<RoomResumeSync>(payload)));
            client.On(RoomCommands.Message, payload => HandleMessage(Decode<RoomMessageEvent>(payload)));

            // currentRooms is intentionally left untouched on OnDisconnected (REQ-019)
            // — resolution happens in HandleConnected on the next reconnect.
            client.OnConnected += HandleConnected;
        }

        /// <summary>Creates a RoomManager bound to <paramref name="client"/>'s room commands.</summary>
        public static RoomManager Attach(Client client, float requestTimeoutSeconds = DefaultRequestTimeoutSeconds)
            => new RoomManager(client, requestTimeoutSeconds);

        /// <summary>Room IDs this client currently believes it's a member of.</summary>
        public IReadOnlyList<string> CurrentRooms => _rooms;

        /// <summary>Requests to join <paramref name="roomId"/>; resolves with the ack's member list.</summary>
        public Task<RoomJoinResult> JoinRoomAsync(string roomId)
        {
            TaskCompletionSource<RoomJoinResult> tcs;
            lock (_pendingLock)
            {
                if (_pendingJoins.ContainsKey(roomId))
                    throw new InvalidOperationException($"join already in flight for room {roomId}");
                tcs = new TaskCompletionSource<RoomJoinResult>(TaskCreationOptions.RunContinuationsAsynchronously);
                _pendingJoins[roomId] = tcs;
            }
            return SendAndAwait(RoomCommands.Join, new RoomJoinRequest { roomId = roomId }, tcs, _pendingJoins, roomId, "join");
        }

        /// <summary>Requests to leave <paramref name="roomId"/>; resolves once acknowledged.</summary>
        public Task LeaveRoomAsync(string roomId)
        {
            TaskCompletionSource<bool> tcs;
            lock (_pendingLock)
            {
                if (_pendingLeaves.ContainsKey(roomId))
                    throw new InvalidOperationException($"leave already in flight for room {roomId}");
                tcs = new TaskCompletionSource<bool>(TaskCreationOptions.RunContinuationsAsynchronously);
                _pendingLeaves[roomId] = tcs;
            }
            return SendAndAwait(RoomCommands.Leave, new RoomLeaveRequest { roomId = roomId }, tcs, _pendingLeaves, roomId, "leave");
        }

        /// <summary>
        /// Sends an arbitrary message to every other member of <paramref name="roomId"/>
        /// (fire-and-forget; no ack). Requires prior <see cref="JoinRoomAsync"/>.
        /// </summary>
        public Task SendMessageAsync(string roomId, string type, string data = null)
            => _client.SendJsonAsync(RoomCommands.Message, new RoomMessageRequest { roomId = roomId, type = type, data = data });

        private async Task<TResult> SendAndAwait<TResult, TReq>(
            uint commandId, TReq request, TaskCompletionSource<TResult> tcs,
            Dictionary<string, TaskCompletionSource<TResult>> pending, string roomId, string verb)
        {
            try
            {
                await _client.SendJsonAsync(commandId, request).ConfigureAwait(false);
            }
            catch (Exception ex)
            {
                lock (_pendingLock) { pending.Remove(roomId); }
                throw new Exception($"failed to send {verb} for room {roomId}: {ex.Message}", ex);
            }

            var timeout = Task.Delay(TimeSpan.FromSeconds(_timeoutSeconds));
            var completed = await Task.WhenAny(tcs.Task, timeout).ConfigureAwait(false);
            if (completed == timeout)
            {
                lock (_pendingLock) { pending.Remove(roomId); }
                throw new TimeoutException($"{verb} room {roomId} timed out after {_timeoutSeconds}s");
            }
            return await tcs.Task.ConfigureAwait(false);
        }

        private void HandleJoinAck(RoomJoinAck ack)
        {
            TaskCompletionSource<RoomJoinResult> tcs;
            lock (_pendingLock) { _pendingJoins.TryGetValue(ack.roomId, out tcs); _pendingJoins.Remove(ack.roomId); }
            tcs?.TrySetResult(new RoomJoinResult { RoomId = ack.roomId, Members = ack.members });
            if (!_rooms.Contains(ack.roomId)) _rooms.Add(ack.roomId);
        }

        private void HandleLeaveAck(RoomLeaveAck ack)
        {
            TaskCompletionSource<bool> tcs;
            lock (_pendingLock) { _pendingLeaves.TryGetValue(ack.roomId, out tcs); _pendingLeaves.Remove(ack.roomId); }
            tcs?.TrySetResult(true);
            _rooms.Remove(ack.roomId);
        }

        private void HandleError(RoomError err)
        {
            var ex = new Exception($"[{err.code}] {err.message}");
            lock (_pendingLock)
            {
                if (_pendingJoins.TryGetValue(err.roomId, out var join))
                {
                    _pendingJoins.Remove(err.roomId);
                    join.TrySetException(ex);
                }
                if (_pendingLeaves.TryGetValue(err.roomId, out var leave))
                {
                    _pendingLeaves.Remove(err.roomId);
                    leave.TrySetException(ex);
                }
            }
        }

        private void HandleMemberEvent(RoomMemberEvent ev)
        {
            switch (ev.type)
            {
                case "joined": OnMemberJoined?.Invoke(ev.roomId, ev.clientId); break;
                case "left": OnMemberLeft?.Invoke(ev.roomId, ev.clientId); break;
                case "disconnected": OnMemberDisconnected?.Invoke(ev.roomId, ev.clientId); break;
                case "reconnected": OnMemberReconnected?.Invoke(ev.roomId, ev.clientId); break;
            }
        }

        private void HandleResumeSync(RoomResumeSync sync)
        {
            // Forward-compatible: unreachable today (see class doc), since the
            // server can only send this after a resumed session, which requires a
            // session id neither client currently persists/resends. If Client
            // gains that, this repopulates rooms after HandleConnected's clear.
            _rooms.Clear();
            if (sync.rooms != null)
                foreach (var r in sync.rooms) _rooms.Add(r.roomId);
        }

        private void HandleMessage(RoomMessageEvent ev)
        {
            OnMessage?.Invoke(ev.roomId, ev.senderId, ev.type, ev.data ?? "");
        }

        private void HandleConnected()
        {
            if (_hasConnectedOnce)
            {
                // A reconnect. No resumable session is possible with the current
                // Client (see class doc / CON-005), so every reconnect is a fresh
                // connection from the server's point of view: drop stale membership.
                _rooms.Clear();
                OnRoomsLost?.Invoke();
            }
            _hasConnectedOnce = true;
        }

        private static T Decode<T>(byte[] payload) => JsonUtility.FromJson<T>(Encoding.UTF8.GetString(payload));
    }
}
