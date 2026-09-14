import { test } from "node:test";
import assert from "node:assert/strict";
import { decodeFrame, encodeFrame, HEADER_SIZE, MAX_PAYLOAD_SIZE, UnsupportedVersionError } from "./protocol.ts";

test("encode/decode roundtrip", () => {
  const payload = new TextEncoder().encode("hello");
  const frame = encodeFrame(0x1234, payload);
  assert.equal(frame.length, HEADER_SIZE + payload.length);

  const decoded = decodeFrame(frame.buffer);
  assert.equal(decoded.version, 1);
  assert.equal(decoded.commandId, 0x1234);
  assert.deepEqual(decoded.payload, payload);
});

test("encodeFrame rejects oversized payload", () => {
  const big = new Uint8Array(MAX_PAYLOAD_SIZE + 1);
  assert.throws(() => encodeFrame(1, big));
});

test("decodeFrame rejects unknown version", () => {
  const frame = new Uint8Array(HEADER_SIZE);
  frame[0] = 9;
  assert.throws(() => decodeFrame(frame.buffer), UnsupportedVersionError);
});

test("decodeFrame rejects short data", () => {
  assert.throws(() => decodeFrame(new Uint8Array(2).buffer));
});
