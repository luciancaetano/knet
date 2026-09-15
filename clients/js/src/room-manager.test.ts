import { test } from "node:test";
import assert from "node:assert/strict";
import { RoomManager } from "./room-manager.ts";
import { RoomCommands } from "./room-protocol.ts";

type CommandHandler = (payload: Uint8Array) => void;
type ConnHandler = (...args: unknown[]) => void;

/** Minimal fake matching the slice of KNetClient's API RoomManager depends on. */
class FakeClient {
  commandHandlers = new Map<number, CommandHandler>();
  connHandlers = new Map<string, Set<ConnHandler>>();
  sent: { commandId: number; payload: unknown }[] = [];
  sendShouldReject = false;

  onCommand(commandId: number, handler: CommandHandler): void {
    this.commandHandlers.set(commandId, handler);
  }

  on(event: string, handler: ConnHandler): void {
    if (!this.connHandlers.has(event)) this.connHandlers.set(event, new Set());
    this.connHandlers.get(event)!.add(handler);
  }

  emit(event: string, ...args: unknown[]): void {
    for (const h of this.connHandlers.get(event) ?? []) h(...args);
  }

  sendJson(commandId: number, payload: unknown): Promise<void> {
    this.sent.push({ commandId, payload });
    if (this.sendShouldReject) return Promise.reject(new Error("send failed"));
    return Promise.resolve();
  }

  deliver(commandId: number, obj: unknown): void {
    const handler = this.commandHandlers.get(commandId);
    assert.ok(handler, `no handler registered for command ${commandId}`);
    handler!(new TextEncoder().encode(JSON.stringify(obj)));
  }
}

test("joinRoom resolves on matching ack and updates currentRooms", async () => {
  const client = new FakeClient();
  const rm = new RoomManager(client as never);

  const promise = rm.joinRoom("room-1");
  client.deliver(RoomCommands.JoinAck, { roomId: "room-1", members: ["a", "b"] });

  const result = await promise;
  assert.deepEqual(result, { roomId: "room-1", members: ["a", "b"] });
  assert.deepEqual(rm.currentRooms, ["room-1"]);
});

test("leaveRoom resolves on matching ack and clears currentRooms", async () => {
  const client = new FakeClient();
  const rm = new RoomManager(client as never);

  client.deliver(RoomCommands.JoinAck, { roomId: "room-1", members: ["a"] });
  const promise = rm.leaveRoom("room-1");
  client.deliver(RoomCommands.LeaveAck, { roomId: "room-1" });

  await promise;
  assert.deepEqual(rm.currentRooms, []);
});

test("joinRoom rejects on matching error", async () => {
  const client = new FakeClient();
  const rm = new RoomManager(client as never);

  const promise = rm.joinRoom("room-1");
  client.deliver(RoomCommands.Error, { roomId: "room-1", code: "ROOM_FULL", message: "full" });

  await assert.rejects(promise, /ROOM_FULL/);
});

test("joinRoom rejects on timeout", async () => {
  const client = new FakeClient();
  const rm = new RoomManager(client as never, { requestTimeoutMs: 10 });

  await assert.rejects(rm.joinRoom("room-1"), /timed out/);
});

test("member events dispatch to the correct handler", () => {
  const client = new FakeClient();
  const rm = new RoomManager(client as never);

  const events: string[] = [];
  rm.on("memberJoined", (roomId, clientId) => events.push(`joined:${roomId}:${clientId}`));
  rm.on("memberLeft", (roomId, clientId) => events.push(`left:${roomId}:${clientId}`));
  rm.on("memberDisconnected", (roomId, clientId) => events.push(`disconnected:${roomId}:${clientId}`));
  rm.on("memberReconnected", (roomId, clientId) => events.push(`reconnected:${roomId}:${clientId}`));

  client.deliver(RoomCommands.MemberEvent, { roomId: "room-1", clientId: "b", type: "joined" });
  client.deliver(RoomCommands.MemberEvent, { roomId: "room-1", clientId: "b", type: "left" });
  client.deliver(RoomCommands.MemberEvent, { roomId: "room-1", clientId: "b", type: "disconnected" });
  client.deliver(RoomCommands.MemberEvent, { roomId: "room-1", clientId: "b", type: "reconnected" });

  assert.deepEqual(events, [
    "joined:room-1:b",
    "left:room-1:b",
    "disconnected:room-1:b",
    "reconnected:room-1:b",
  ]);
});

test("sendMessage sends via sendJson and message events dispatch", async () => {
  const client = new FakeClient();
  const rm = new RoomManager(client as never);

  const events: string[] = [];
  rm.on("message", (roomId, senderId, type, data) => events.push(`${roomId}:${senderId}:${type}:${data}`));

  await rm.sendMessage("room-1", "chat", "hi");
  assert.deepEqual(client.sent[0], { commandId: RoomCommands.Message, payload: { roomId: "room-1", type: "chat", data: "hi" } });

  client.deliver(RoomCommands.Message, { roomId: "room-1", senderId: "b", type: "chat", data: "hi" });
  assert.deepEqual(events, ["room-1:b:chat:hi"]);
});

test("reconnect clears currentRooms and emits roomsLost", () => {
  const client = new FakeClient();
  const rm = new RoomManager(client as never);

  client.deliver(RoomCommands.JoinAck, { roomId: "room-1", members: ["a"] });
  assert.deepEqual(rm.currentRooms, ["room-1"]);

  let lostCount = 0;
  rm.on("roomsLost", () => lostCount++);

  // First "connected" is the initial connection, not a reconnect.
  client.emit("connected");
  assert.equal(lostCount, 0);
  assert.deepEqual(rm.currentRooms, ["room-1"]);

  // Second "connected" is a reconnect: rooms are lost.
  client.emit("connected");
  assert.equal(lostCount, 1);
  assert.deepEqual(rm.currentRooms, []);
});
