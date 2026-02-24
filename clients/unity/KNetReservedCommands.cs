namespace Knet
{
    /// <summary>
    /// Command IDs reserved by the knet protocol.
    /// User command IDs must be below <see cref="CommandError"/>.
    /// </summary>
    public static class KNetReservedCommands
    {
        /// <summary>JSON-RPC 2.0 request/response envelope.</summary>
        public const uint JsonRpc = 0xFFFFFFFF;

        /// <summary>JSON-RPC 2.0 error response envelope.</summary>
        public const uint JsonRpcError = 0xFFFFFFFE;

        /// <summary>Server sent an unrecognised command ID.</summary>
        public const uint InvalidCommand = 0xFFFFFFFD;

        /// <summary>Server encountered an error processing a command.</summary>
        public const uint CommandError = 0xFFFFFFFC;
    }
}
