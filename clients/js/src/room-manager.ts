import type { KNetClient } from "./client.js";
import {
  RoomCommands,
  type RoomError,
  type RoomJoinAck,
  type RoomLeaveAck,
  type RoomMemberEvent,
  type RoomMessageEvent,
  type RoomResumeSync,
} from "./room-protocol.js";

const DEFAULT_REQUEST_TIMEOUT_MS = 10_000;

export interface RoomManagerOptions {
  requestTimeoutMs?: number;
}

export interface RoomJoinResult {
  roomId: string;
  members: string[];
}

interface PendingRequest<T> {
  resolve: (value: T) => void;
  reject: (err: Error) => void;
  timer: ReturnType<typeof setTimeout>;
}

interface RoomManagerEventMap {
  memberJoined: (roomId: string, clientId: string) => void;
  memberLeft: (roomId: string, clientId: string) => void;
  memberDisconnected: (roomId: string, clientId: string) => void;
  memberReconnected: (roomId: string, clientId: string) => void;
  roomsLost: () => void;
  message: (roomId: string, senderId: string, type: string, data: string) => void;
}

const textDecoder = new TextDecoder();

function decodeJson<T>(payload: Uint8Array): T {
  return JSON.parse(textDecoder.decode(payload)) as T;
}

/**
 * Optional room join/leave/membership layer on top of KNetClient, wrapping
 * the wire protocol defined in roommanager/protocol.go (Go server side).
 *
 * Reconnect caveat: KNetClient does not currently persist/resend a session
 * ID across reconnects, so the server can never treat a reconnect as a
 * resume (see spec/spec-architecture-room-manager.md CON-005). Every
 * reconnect is therefore treated as a fresh connection: currentRooms is
 * cleared and `roomsLost` is emitted. The CmdRoomResumeSync handler is
 * wired for forward-compatibility but is unreachable with the client as it
 * exists today.
 */
export class RoomManager {
  private readonly client: KNetClient;
  private readonly timeoutMs: number;

  private rooms = new Set<string>();
  private hasConnectedOnce = false;

  private readonly pendingJoins = new Map<string, PendingRequest<RoomJoinResult>>();
  private readonly pendingLeaves = new Map<string, PendingRequest<void>>();

  private readonly listeners: { [K in keyof RoomManagerEventMap]: Set<RoomManagerEventMap[K]> } = {
    memberJoined: new Set(),
    memberLeft: new Set(),
    memberDisconnected: new Set(),
    memberReconnected: new Set(),
    roomsLost: new Set(),
    message: new Set(),
  };

  constructor(client: KNetClient, opts: RoomManagerOptions = {}) {
    this.client = client;
    this.timeoutMs = opts.requestTimeoutMs ?? DEFAULT_REQUEST_TIMEOUT_MS;

    client.onCommand(RoomCommands.JoinAck, (payload) => this.handleJoinAck(decodeJson(payload)));
    client.onCommand(RoomCommands.LeaveAck, (payload) => this.handleLeaveAck(decodeJson(payload)));
    client.onCommand(RoomCommands.Error, (payload) => this.handleError(decodeJson(payload)));
    client.onCommand(RoomCommands.MemberEvent, (payload) => this.handleMemberEvent(decodeJson(payload)));
    client.onCommand(RoomCommands.ResumeSync, (payload) => this.handleResumeSync(decodeJson(payload)));
    client.onCommand(RoomCommands.Message, (payload) => this.handleMessage(decodeJson(payload)));

    // currentRooms is intentionally left untouched on "disconnected" (REQ-019)
    // — resolution happens in handleConnected on the next reconnect.
    client.on("connected", () => this.handleConnected());
  }

  get currentRooms(): readonly string[] {
    return Array.from(this.rooms);
  }

  joinRoom(roomId: string): Promise<RoomJoinResult> {
    if (this.pendingJoins.has(roomId)) {
      return Promise.reject(new Error(`join already in flight for room ${roomId}`));
    }
    const promise = new Promise<RoomJoinResult>((resolve, reject) => {
      const timer = setTimeout(() => {
        this.pendingJoins.delete(roomId);
        reject(new Error(`join room ${roomId} timed out after ${this.timeoutMs}ms`));
      }, this.timeoutMs);
      this.pendingJoins.set(roomId, { resolve, reject, timer });
    });
    this.client.sendJson(RoomCommands.Join, { roomId }).catch((err) => {
      const pending = this.pendingJoins.get(roomId);
      if (!pending) return;
      this.pendingJoins.delete(roomId);
      clearTimeout(pending.timer);
      pending.reject(err instanceof Error ? err : new Error(String(err)));
    });
    return promise;
  }

  leaveRoom(roomId: string): Promise<void> {
    if (this.pendingLeaves.has(roomId)) {
      return Promise.reject(new Error(`leave already in flight for room ${roomId}`));
    }
    const promise = new Promise<void>((resolve, reject) => {
      const timer = setTimeout(() => {
        this.pendingLeaves.delete(roomId);
        reject(new Error(`leave room ${roomId} timed out after ${this.timeoutMs}ms`));
      }, this.timeoutMs);
      this.pendingLeaves.set(roomId, { resolve, reject, timer });
    });
    this.client.sendJson(RoomCommands.Leave, { roomId }).catch((err) => {
      const pending = this.pendingLeaves.get(roomId);
      if (!pending) return;
      this.pendingLeaves.delete(roomId);
      clearTimeout(pending.timer);
      pending.reject(err instanceof Error ? err : new Error(String(err)));
    });
    return promise;
  }

  sendMessage(roomId: string, type: string, data?: string): Promise<void> {
    return this.client.sendJson(RoomCommands.Message, { roomId, type, data });
  }

  on<K extends keyof RoomManagerEventMap>(event: K, handler: RoomManagerEventMap[K]): void {
    this.listeners[event].add(handler);
  }

  off<K extends keyof RoomManagerEventMap>(event: K, handler: RoomManagerEventMap[K]): void {
    this.listeners[event].delete(handler);
  }

  private emit<K extends keyof RoomManagerEventMap>(event: K, ...args: Parameters<RoomManagerEventMap[K]>): void {
    for (const handler of this.listeners[event]) {
      (handler as (...a: Parameters<RoomManagerEventMap[K]>) => void)(...args);
    }
  }

  private handleJoinAck(ack: RoomJoinAck): void {
    const pending = this.pendingJoins.get(ack.roomId);
    if (pending) {
      this.pendingJoins.delete(ack.roomId);
      clearTimeout(pending.timer);
      pending.resolve({ roomId: ack.roomId, members: ack.members });
    }
    this.rooms.add(ack.roomId);
  }

  private handleLeaveAck(ack: RoomLeaveAck): void {
    const pending = this.pendingLeaves.get(ack.roomId);
    if (pending) {
      this.pendingLeaves.delete(ack.roomId);
      clearTimeout(pending.timer);
      pending.resolve();
    }
    this.rooms.delete(ack.roomId);
  }

  private handleError(err: RoomError): void {
    const join = this.pendingJoins.get(err.roomId);
    if (join) {
      this.pendingJoins.delete(err.roomId);
      clearTimeout(join.timer);
      join.reject(new Error(`[${err.code}] ${err.message}`));
    }
    const leave = this.pendingLeaves.get(err.roomId);
    if (leave) {
      this.pendingLeaves.delete(err.roomId);
      clearTimeout(leave.timer);
      leave.reject(new Error(`[${err.code}] ${err.message}`));
    }
  }

  private handleMemberEvent(ev: RoomMemberEvent): void {
    switch (ev.type) {
      case "joined":
        this.emit("memberJoined", ev.roomId, ev.clientId);
        break;
      case "left":
        this.emit("memberLeft", ev.roomId, ev.clientId);
        break;
      case "disconnected":
        this.emit("memberDisconnected", ev.roomId, ev.clientId);
        break;
      case "reconnected":
        this.emit("memberReconnected", ev.roomId, ev.clientId);
        break;
    }
  }

  private handleMessage(ev: RoomMessageEvent): void {
    this.emit("message", ev.roomId, ev.senderId, ev.type, ev.data ?? "");
  }

  private handleResumeSync(sync: RoomResumeSync): void {
    // Forward-compatible: unreachable today (see class doc), since the
    // server can only send this after a resumed session, which requires a
    // ?session= id neither client currently persists/resends. If a future
    // KNetClient gains that, this repopulates rooms after handleConnected's
    // clear below — correct once ordering allows it.
    this.rooms = new Set(sync.rooms.map((r) => r.roomId));
  }

  private handleConnected(): void {
    if (this.hasConnectedOnce) {
      // A reconnect. No resumable session is possible with the current
      // KNetClient (see class doc / CON-005), so every reconnect is a fresh
      // connection from the server's point of view: drop stale membership.
      this.rooms.clear();
      this.emit("roomsLost");
    }
    this.hasConnectedOnce = true;
  }
}
