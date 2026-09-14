package main

// --8<-- [start:commands]
// Command IDs for the chat protocol. User-defined IDs must stay below
// 0xFFFFFFFC (the range reserved by knet itself — see the Reference page).
const (
	CmdSetName          uint32 = 0x0001 // client -> server: desired display name (UTF-8 text)
	CmdChat             uint32 = 0x0002 // client -> server -> room: chat message
	CmdUserJoined       uint32 = 0x0003 // server -> room: someone joined
	CmdUserLeft         uint32 = 0x0004 // server -> room: someone left voluntarily
	CmdUserDisconnected uint32 = 0x0005 // server -> room: someone dropped unexpectedly
)

// --8<-- [end:commands]

// chatMessage is the JSON payload broadcast on CmdChat.
type chatMessage struct {
	From string `json:"from"`
	Text string `json:"text"`
}

// presenceMessage is the JSON payload broadcast on CmdUserJoined,
// CmdUserLeft and CmdUserDisconnected.
type presenceMessage struct {
	Name string `json:"name"`
}
