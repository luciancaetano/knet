// Reserved command IDs and payload shapes for RoomManager's join/leave/event
// protocol. Mirrors roommanager/protocol.go — keep field names and command
// IDs in sync manually.

export const RoomCommands = {
  Join: 0xfffe0001,
  Leave: 0xfffe0002,
  JoinAck: 0xfffe0003,
  LeaveAck: 0xfffe0004,
  Error: 0xfffe0005,
  MemberEvent: 0xfffe0006,
  ResumeSync: 0xfffe0007,
  Message: 0xfffe0008,
} as const;

export const RoomErrorCode = {
  RoomFull: "ROOM_FULL",
  JoinRejected: "JOIN_REJECTED",
  NotAMember: "NOT_A_MEMBER",
  InvalidPayload: "INVALID_PAYLOAD",
} as const;

export type RoomMemberEventType = "joined" | "left" | "disconnected" | "reconnected";

export interface RoomJoinRequest {
  roomId: string;
}

export interface RoomLeaveRequest {
  roomId: string;
}

export interface RoomJoinAck {
  roomId: string;
  members: string[];
}

export interface RoomLeaveAck {
  roomId: string;
}

export interface RoomError {
  roomId: string;
  code: string;
  message: string;
}

export interface RoomMemberEvent {
  roomId: string;
  clientId: string;
  type: RoomMemberEventType;
}

export interface RoomResumeSyncEntry {
  roomId: string;
  members: string[];
}

export interface RoomResumeSync {
  rooms: RoomResumeSyncEntry[];
}

export interface RoomMessageRequest {
  roomId: string;
  type: string;
  data?: string;
}

export interface RoomMessageEvent {
  roomId: string;
  senderId: string;
  type: string;
  data?: string;
}
