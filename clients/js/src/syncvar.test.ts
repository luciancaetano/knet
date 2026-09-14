import { test } from "node:test";
import assert from "node:assert/strict";
import { decodeSyncVar, encodeSyncVar, SyncVarType } from "./syncvar.ts";

test("encode/decode roundtrip all types", () => {
  const cases: [unknown, number][] = [
    [42, SyncVarType.Int32],
    [3.5, SyncVarType.Float64],
    [true, SyncVarType.Bool],
    ["hello", SyncVarType.String],
  ];
  for (const [value, tag] of cases) {
    const encoded = encodeSyncVar(value);
    assert.equal(encoded[0], tag, `tag for ${value}`);
    assert.deepEqual(decodeSyncVar(encoded), value, `roundtrip ${value}`);
  }
});

test("int64 roundtrips as BigInt", () => {
  const encoded = encodeSyncVar(12345678901234);
  assert.equal(encoded[0], SyncVarType.Int64);
  assert.equal(decodeSyncVar(encoded), 12345678901234n);
});

test("encodeSyncVar rejects unsupported types", () => {
  assert.throws(() => encodeSyncVar({}), TypeError);
  assert.throws(() => encodeSyncVar(null), TypeError);
  assert.throws(() => encodeSyncVar(undefined), TypeError);
});

test("decodeSyncVar rejects malformed payload", () => {
  assert.throws(() => decodeSyncVar(new Uint8Array(0)), TypeError);
  assert.throws(() => decodeSyncVar(new Uint8Array([0xff, 0x31])), TypeError);
});