// Wire format: [1B version][4B BE commandID][payload]
// Mirrors internal/protocol/protocol.go.

export const CURRENT_VERSION = 1;
export const HEADER_SIZE = 5;
export const MAX_PAYLOAD_SIZE = 10 * 1024 * 1024;

export const ReservedCommands = {
  JsonRpc: 0xffffffff,
  JsonRpcError: 0xfffffffe,
  Ping: 0xfffffffd,
} as const;

export class UnsupportedVersionError extends Error {
  readonly version: number;
  constructor(version: number) {
    super(`unsupported protocol version: ${version}`);
    this.version = version;
  }
}

export function encodeFrame(commandId: number, payload: Uint8Array): Uint8Array {
  if (payload.length > MAX_PAYLOAD_SIZE) {
    throw new Error(`payload size ${payload.length} exceeds maximum ${MAX_PAYLOAD_SIZE} bytes`);
  }
  const out = new Uint8Array(HEADER_SIZE + payload.length);
  const view = new DataView(out.buffer);
  view.setUint8(0, CURRENT_VERSION);
  view.setUint32(1, commandId, false);
  out.set(payload, HEADER_SIZE);
  return out;
}

export interface DecodedFrame {
  version: number;
  commandId: number;
  payload: Uint8Array;
}

export function decodeFrame(data: ArrayBufferLike): DecodedFrame {
  if (data.byteLength < HEADER_SIZE) {
    throw new Error("data too short");
  }
  const view = new DataView(data);
  const version = view.getUint8(0);
  if (version !== CURRENT_VERSION) {
    throw new UnsupportedVersionError(version);
  }
  const commandId = view.getUint32(1, false);
  const payload = new Uint8Array(data.slice(HEADER_SIZE));
  return { version, commandId, payload };
}
