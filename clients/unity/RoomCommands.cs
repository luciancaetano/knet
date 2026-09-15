namespace Knet
{
    /// <summary>
    /// Reserved command IDs for the RoomManager join/leave/membership protocol.
    /// Mirrors the Go server (<c>roommanager/protocol.go</c>) and the JS client
    /// (<c>room-protocol.ts</c>) — keep these three in sync manually.
    /// </summary>
    public static class RoomCommands
    {
        /// <summary>Client → server: request to join a room.</summary>
        public const uint Join = 0xFFFE0001;

        /// <summary>Client → server: request to leave a room.</summary>
        public const uint Leave = 0xFFFE0002;

        /// <summary>Server → client: join acknowledgement.</summary>
        public const uint JoinAck = 0xFFFE0003;

        /// <summary>Server → client: leave acknowledgement.</summary>
        public const uint LeaveAck = 0xFFFE0004;

        /// <summary>Server → client: room operation error.</summary>
        public const uint Error = 0xFFFE0005;

        /// <summary>Server → client: member joined/left/disconnected/reconnected.</summary>
        public const uint MemberEvent = 0xFFFE0006;

        /// <summary>Server → client: resumed session's room membership sync.</summary>
        public const uint ResumeSync = 0xFFFE0007;

        /// <summary>Client → server to send, server → client to deliver, an arbitrary room message.</summary>
        public const uint Message = 0xFFFE0008;
    }
}
