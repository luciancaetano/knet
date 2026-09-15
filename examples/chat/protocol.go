package main

// --8<-- [start:commands]
// Command IDs for the chat protocol. User-defined IDs must stay below
// 0xFFFFFFFC (the range reserved by knet itself — see the Reference page).
// Chat itself and room join/leave/presence are handled by roommanager's own
// reserved commands (0xFFFE0001-0xFFFE0008) — this example only needs one
// custom command, for the display name RoomManager has no concept of.
const (
	CmdSetName    uint32 = 0x0001 // client -> server: desired display name (UTF-8 text)
	CmdUserJoined uint32 = 0x0003 // server -> room: someone joined, with their name
	CmdUserLeft   uint32 = 0x0004 // server -> room: someone left voluntarily, with their name
)

// --8<-- [end:commands]

// presenceMessage is the JSON payload broadcast on CmdUserJoined/CmdUserLeft.
type presenceMessage struct {
	Name string `json:"name"`
}
