namespace Knet
{
    /// <summary>
    /// Represents the current state of a KNetClient connection.
    /// </summary>
    public enum KNetConnectionState
    {
        /// <summary>Not connected and not attempting to connect.</summary>
        Disconnected,

        /// <summary>Attempting to open a WebSocket connection.</summary>
        Connecting,

        /// <summary>WebSocket is open and the client is ready to send/receive.</summary>
        Connected,

        /// <summary>Connection was lost; waiting before the next reconnect attempt.</summary>
        Reconnecting,

        /// <summary>Disconnect() was called; auto-reconnect is permanently disabled.</summary>
        Closed,
    }
}
