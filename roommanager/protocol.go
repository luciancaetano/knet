package roommanager

import "encoding/json"

// Reserved command IDs for RoomManager's join/leave/event protocol.
// Distinct from knet's own reserved IDs (knet.CmdJSONRPC, knet.CmdJSONRPCError)
// and from the ad-hoc ranges already in use by the JS/Unity clients
// (see spec §7), so RoomManager claims the 0xFFFE00xx sub-range.
const (
	// CmdRoomJoin is sent client → server to join a room.
	CmdRoomJoin uint32 = 0xFFFE0001
	// CmdRoomLeave is sent client → server to leave a room.
	CmdRoomLeave uint32 = 0xFFFE0002
	// CmdRoomJoinAck is sent server → client on successful join.
	CmdRoomJoinAck uint32 = 0xFFFE0003
	// CmdRoomLeaveAck is sent server → client on successful leave.
	CmdRoomLeaveAck uint32 = 0xFFFE0004
	// CmdRoomError is sent server → client when a join/leave is rejected.
	CmdRoomError uint32 = 0xFFFE0005
	// CmdRoomMemberEvent is broadcast server → clients on membership changes.
	CmdRoomMemberEvent uint32 = 0xFFFE0006
	// CmdRoomResumeSync is sent server → client after an automatic
	// resume-triggered rejoin, in place of per-room join acks.
	CmdRoomResumeSync uint32 = 0xFFFE0007
	// CmdRoomMessage is sent client → server to broadcast an arbitrary
	// message to a room, and server → clients (other members) to deliver it.
	CmdRoomMessage uint32 = 0xFFFE0008
)

// Room error codes (RoomError.Code).
const (
	ErrCodeRoomFull        = "ROOM_FULL"
	ErrCodeJoinRejected    = "JOIN_REJECTED"
	ErrCodeNotAMember      = "NOT_A_MEMBER"
	ErrCodeInvalidPayload  = "INVALID_PAYLOAD"
	ErrCodeMessageRejected = "MESSAGE_REJECTED"
)

// Member event types (RoomMemberEvent.Type).
const (
	MemberJoined       = "joined"
	MemberLeft         = "left"
	MemberDisconnected = "disconnected"
	MemberReconnected  = "reconnected"
)

// RoomJoinRequest is the payload of CmdRoomJoin.
type RoomJoinRequest struct {
	RoomID string `json:"roomId"`
	// RoomType names a handler registered via Manager.Define. Empty means no
	// handler is attached to this room (legacy flat mode).
	RoomType string `json:"roomType,omitempty"`
	// Metadata is opaque, app-defined join-time data (e.g. a display name),
	// handed to RoomHandler.OnJoin so an application can act on it before the
	// join is observable by other members — avoiding a separate side-channel
	// call racing the join itself.
	Metadata json.RawMessage `json:"metadata,omitempty"`
}

// RoomLeaveRequest is the payload of CmdRoomLeave.
type RoomLeaveRequest struct {
	RoomID string `json:"roomId"`
}

// RoomJoinAck is the payload of CmdRoomJoinAck.
type RoomJoinAck struct {
	RoomID  string   `json:"roomId"`
	Members []string `json:"members"`
}

// RoomLeaveAck is the payload of CmdRoomLeaveAck.
type RoomLeaveAck struct {
	RoomID string `json:"roomId"`
}

// RoomError is the payload of CmdRoomError.
type RoomError struct {
	RoomID  string `json:"roomId"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// RoomMemberEvent is the payload of CmdRoomMemberEvent.
type RoomMemberEvent struct {
	RoomID   string `json:"roomId"`
	ClientID string `json:"clientId"`
	Type     string `json:"type"`
}

// RoomResumeSync is the payload of CmdRoomResumeSync.
type RoomResumeSync struct {
	Rooms []RoomResumeSyncEntry `json:"rooms"`
}

// RoomResumeSyncEntry is one room's membership within a RoomResumeSync.
type RoomResumeSyncEntry struct {
	RoomID  string   `json:"roomId"`
	Members []string `json:"members"`
}

// RoomMessageRequest is the payload of CmdRoomMessage sent client → server.
// Data is an opaque, app-defined string (e.g. a JSON-encoded value) so
// RoomManager itself doesn't need to know the message shape.
type RoomMessageRequest struct {
	RoomID string `json:"roomId"`
	Type   string `json:"type"`
	Data   string `json:"data,omitempty"`
}

// RoomMessageEvent is the payload of CmdRoomMessage broadcast server →
// clients (all members of RoomID except the sender).
type RoomMessageEvent struct {
	RoomID   string `json:"roomId"`
	SenderID string `json:"senderId"`
	Type     string `json:"type"`
	Data     string `json:"data,omitempty"`
}
