package main

// --8<-- [start:commands]
// Command IDs for the chat protocol. User-defined IDs must stay below
// 0xFFFFFFFC (the range reserved by knet itself — see the Reference page).
// Chat itself, room join/leave, and presence all ride roommanager's own
// reserved commands (0xFFFE0001-0xFFFE0008) — this example needs no custom
// commands: the display name travels as join metadata (see joinMetadata).
const (
	CmdUserJoined   uint32 = 0x0003 // server -> room: someone joined, with their name
	CmdUserLeft     uint32 = 0x0004 // server -> room: someone left the room (explicit leave, or grace period expired), with their name
	CmdRoomSnapshot uint32 = 0x0005 // server -> joining client only: names of members already in the room
)

// --8<-- [end:commands]

// joinMetadata is the JSON payload the client attaches to RoomManager's join
// call (RoomJoinRequest.Metadata) — the one piece of application data
// roommanager itself doesn't know about.
type joinMetadata struct {
	Name string `json:"name"`
}

// presenceMessage is the JSON payload broadcast on CmdUserJoined/CmdUserLeft.
// ClientID lets the client map senderId (all it gets on chat messages) to a
// display name.
type presenceMessage struct {
	ClientID string `json:"clientId"`
	Name     string `json:"name"`
}

// roomSnapshot is sent (CmdRoomSnapshot) to a client right as it joins,
// listing everyone already in the room — CmdUserJoined only ever fires for
// members who join *after* you, so without this a joiner has no way to learn
// the names of members already present.
type roomSnapshot struct {
	Members []presenceMessage `json:"members"`
}
