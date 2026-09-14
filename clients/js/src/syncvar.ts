// Wire format for sync vars (mirrors syncvar/syncvar.go):
//   [0]     1-byte type tag
//   [1..]   value as UTF-8 text (decimal digits, strconv-style floats, raw for string)
//
// Tags must match Go's syncvar.Tag values.

export const SyncVarType = {
  Int32: 0x01,
  Int64: 0x02,
  Float32: 0x03,
  Float64: 0x04,
  Bool: 0x05,
  String: 0x06,
} as const;

export type SyncVarTypeValue = (typeof SyncVarType)[keyof typeof SyncVarType];

function textBytes(tag: SyncVarTypeValue, text: string): Uint8Array {
  const body = new TextEncoder().encode(text);
  const out = new Uint8Array(1 + body.length);
  out[0] = tag;
  out.set(body, 1);
  return out;
}

/** Encodes a value as a tagged sync var payload. Throws on unsupported types. */
export function encodeSyncVar(value: unknown): Uint8Array {
  switch (typeof value) {
    case "number":
      // Go emits int32/int64 as decimal digits and float as 'f' -1 text.
      return textBytes(
        Number.isInteger(value)
          ? value >= -0x80000000 && value <= 0x7fffffff
            ? SyncVarType.Int32
            : SyncVarType.Int64
          : SyncVarType.Float64,
        String(value),
      );
    case "boolean":
      return textBytes(SyncVarType.Bool, String(value));
    case "string":
      return textBytes(SyncVarType.String, value);
    default:
      throw new TypeError(
        `sync var: unsupported JS type '${typeof value}' (wire supports Int32/Int64/Float32/Float64/Bool/String)`,
      );
  }
}

/**
 * Decodes a tagged sync var payload to its JS value.
 * Throws `TypeError` if payload is malformed or empty.
 */
export function decodeSyncVar(payload: Uint8Array): unknown {
  if (payload.length < 1) throw new TypeError("sync var: empty payload");
  const tag = payload[0];
  const text = new TextDecoder().decode(payload.subarray(1));

  switch (tag) {
    case SyncVarType.Int32:
      return parseInt(text, 10);
    case SyncVarType.Int64:
      return BigInt(text);
    case SyncVarType.Float32:
      return Math.fround(parseFloat(text));
    case SyncVarType.Float64:
      return parseFloat(text);
    case SyncVarType.Bool:
      return text === "true";
    case SyncVarType.String:
      return text;
    default:
      throw new TypeError(`sync var: unknown tag 0x${tag.toString(16)}`);
  }
}