using System;

namespace Knet
{
    /// <summary>
    /// Encode and decode messages in the knet binary wire format.
    ///
    /// Wire format:
    ///   [0]     Version   — byte, must equal CurrentVersion
    ///   [1..4]  CommandID — uint32, big-endian
    ///   [5..]   Payload   — arbitrary bytes (0 … MaxPayloadSize)
    /// </summary>
    public static class KNetProtocol
    {
        /// <summary>Wire protocol version this client speaks (must match the server's protocol.CurrentVersion).</summary>
        public const byte CurrentVersion = 1;

        /// <summary>Size of the fixed version+command-ID header in bytes.</summary>
        public const int HeaderSize = 5;

        /// <summary>Maximum allowed payload size (10 MiB, matching the server limit).</summary>
        public const int MaxPayloadSize = 10 * 1024 * 1024;

        /// <summary>
        /// Encodes a command ID and payload into a single byte array ready to send.
        /// </summary>
        /// <param name="commandId">The command identifier (must not be in the reserved range
        /// unless you are constructing a JSON-RPC message).</param>
        /// <param name="payload">The message body. May be empty but must not exceed
        /// <see cref="MaxPayloadSize"/> bytes.</param>
        /// <returns>A new byte array containing the version byte, 4-byte header, and payload.</returns>
        /// <exception cref="ArgumentException">Thrown when the payload exceeds the size limit.</exception>
        public static byte[] Encode(uint commandId, byte[] payload)
        {
            if (payload == null)
                payload = Array.Empty<byte>();

            if (payload.Length > MaxPayloadSize)
                throw new ArgumentException(
                    $"Payload length {payload.Length} exceeds MaxPayloadSize ({MaxPayloadSize}).",
                    nameof(payload));

            var result = new byte[HeaderSize + payload.Length];

            result[0] = CurrentVersion;

            // Write CommandID as big-endian uint32.
            result[1] = (byte)(commandId >> 24);
            result[2] = (byte)(commandId >> 16);
            result[3] = (byte)(commandId >> 8);
            result[4] = (byte)(commandId);

            if (payload.Length > 0)
                Buffer.BlockCopy(payload, 0, result, HeaderSize, payload.Length);

            return result;
        }

        /// <summary>
        /// Decodes a raw WebSocket message into a command ID and payload.
        /// The returned payload is an independent copy; mutating it does not affect
        /// the original buffer.
        /// </summary>
        /// <param name="data">The raw bytes received from the WebSocket connection.</param>
        /// <returns>A tuple of (commandId, payload).</returns>
        /// <exception cref="ArgumentException">Thrown when the data is shorter than the header.</exception>
        /// <exception cref="InvalidOperationException">Thrown when the version byte does not match <see cref="CurrentVersion"/>.</exception>
        public static (uint commandId, byte[] payload) Decode(byte[] data)
        {
            if (data == null || data.Length < HeaderSize)
                throw new ArgumentException(
                    $"Message too short: expected at least {HeaderSize} bytes, got {data?.Length ?? 0}.",
                    nameof(data));

            byte version = data[0];
            if (version != CurrentVersion)
                throw new InvalidOperationException(
                    $"Unsupported protocol version: {version} (expected {CurrentVersion}).");

            uint commandId = ((uint)data[1] << 24)
                           | ((uint)data[2] << 16)
                           | ((uint)data[3] << 8)
                           |  (uint)data[4];

            int payloadLength = data.Length - HeaderSize;
            var payload = new byte[payloadLength];
            if (payloadLength > 0)
                Buffer.BlockCopy(data, HeaderSize, payload, 0, payloadLength);

            return (commandId, payload);
        }
    }
}
