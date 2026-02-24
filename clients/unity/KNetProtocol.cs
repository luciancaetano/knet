using System;

namespace Knet
{
    /// <summary>
    /// Encode and decode messages in the knet binary wire format.
    ///
    /// Wire format:
    ///   [0..3]  CommandID — uint32, big-endian
    ///   [4..]   Payload   — arbitrary bytes (0 … MaxPayloadSize)
    /// </summary>
    public static class KNetProtocol
    {
        /// <summary>Size of the fixed command-ID header in bytes.</summary>
        public const int HeaderSize = 4;

        /// <summary>Maximum allowed payload size (10 MiB, matching the server limit).</summary>
        public const int MaxPayloadSize = 10 * 1024 * 1024;

        /// <summary>
        /// Encodes a command ID and payload into a single byte array ready to send.
        /// </summary>
        /// <param name="commandId">The command identifier (must not be in the reserved range
        /// unless you are constructing a JSON-RPC message).</param>
        /// <param name="payload">The message body. May be empty but must not exceed
        /// <see cref="MaxPayloadSize"/> bytes.</param>
        /// <returns>A new byte array containing the 4-byte header followed by the payload.</returns>
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

            // Write CommandID as big-endian uint32.
            result[0] = (byte)(commandId >> 24);
            result[1] = (byte)(commandId >> 16);
            result[2] = (byte)(commandId >> 8);
            result[3] = (byte)(commandId);

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
        public static (uint commandId, byte[] payload) Decode(byte[] data)
        {
            if (data == null || data.Length < HeaderSize)
                throw new ArgumentException(
                    $"Message too short: expected at least {HeaderSize} bytes, got {data?.Length ?? 0}.",
                    nameof(data));

            uint commandId = ((uint)data[0] << 24)
                           | ((uint)data[1] << 16)
                           | ((uint)data[2] << 8)
                           |  (uint)data[3];

            int payloadLength = data.Length - HeaderSize;
            var payload = new byte[payloadLength];
            if (payloadLength > 0)
                Buffer.BlockCopy(data, HeaderSize, payload, 0, payloadLength);

            return (commandId, payload);
        }
    }
}
