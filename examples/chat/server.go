package main

import (
	"context"
	"encoding/json"
	"log"

	"github.com/luciancaetano/knet"
	"github.com/luciancaetano/knet/internal/room"
	"github.com/luciancaetano/knet/roommanager"
)

// --8<-- [start:server-type]
// chatServer holds state that isn't scoped to a single room: the RoomManager
// itself, and the display name chosen by each connected client (RoomManager
// only knows clientIDs). roomManager starts nil and is set once, in main()
// (2.8) — it needs the *ws.Server, which doesn't exist yet here.
type chatServer struct {
	roomManager *roommanager.Manager
	names       *nameStore
}

func newChatServer() *chatServer {
	return &chatServer{names: newNameStore()}
}

// --8<-- [end:server-type]

// --8<-- [start:lobby-room]
// lobbyRoom is a roommanager.RoomHandler: everything about the "lobby" room
// type lives in this one type, the same shape as a Colyseus Room class
// (OnCreate/OnJoin/OnLeave/OnDispose). One instance is created per room
// instance by the Manager — see main.go's roomManager.Define("lobby", ...).
type lobbyRoom struct {
	view  room.View // this room's broadcast handle, set in OnCreate
	names *nameStore
}

// OnCreate runs once, the first time a client joins this room instance.
func (r *lobbyRoom) OnCreate(v room.View) {
	r.view = v
}

// OnJoin runs for every client that joins, including the one that triggers
// OnCreate above (OnCreate then OnJoin, in that order). metadata is the
// join's payload — here, {"name": "..."} — sent as part of the join itself
// (RoomManager.joinRoom's metadata argument), so the name is stored before
// this room is created or broadcasts anything: no race against a separate
// SetName call that the room can't see or order against.
func (r *lobbyRoom) OnJoin(client knet.Client, metadata json.RawMessage) {
	var join joinMetadata
	_ = json.Unmarshal(metadata, &join)
	if join.Name == "" {
		join.Name = "guest"
	}
	r.names.set(client.ID(), join.Name)
	r.sendSnapshot(client)
	r.broadcastPresence(CmdUserJoined, client.ID())
}

// sendSnapshot tells the newly-joined client the names of everyone already
// in the room. CmdUserJoined only ever broadcasts for members who join after
// you, so without this a client joining a populated room would never learn
// existing members' names — not a timing race, a permanent gap. Sent before
// broadcastPresence below, so it's in place before this client can receive
// any chat message referencing another member's clientID.
func (r *lobbyRoom) sendSnapshot(client knet.Client) {
	members := make([]presenceMessage, 0, len(r.view.Clients()))
	for _, c := range r.view.Clients() {
		if c.ID() == client.ID() {
			continue
		}
		members = append(members, presenceMessage{ClientID: c.ID(), Name: r.names.get(c.ID())})
	}
	payload, _ := json.Marshal(roomSnapshot{Members: members})
	if err := client.Send(context.Background(), CmdRoomSnapshot, payload); err != nil {
		log.Printf("send room snapshot failed: %v", err)
	}
}

// OnLeave runs for every client that leaves — explicit leave, voluntary
// disconnect, or an involuntary drop whose grace period expired without a
// reconnect. All three cases already reach here the same way; see
// "Voluntary vs. involuntary exit" in the docs for how that's decided.
func (r *lobbyRoom) OnLeave(client knet.Client) {
	r.broadcastPresence(CmdUserLeft, client.ID())
}

// OnDispose runs once, when the room closes (membership hits zero). Nothing
// to clean up here — lobbyRoom holds no state beyond the shared *nameStore.
func (r *lobbyRoom) OnDispose() {}

func (r *lobbyRoom) broadcastPresence(cmd uint32, clientID string) {
	payload, _ := json.Marshal(presenceMessage{ClientID: clientID, Name: r.names.get(clientID)})
	if err := r.view.Broadcast(context.Background(), cmd, payload); err != nil {
		log.Printf("broadcast presence failed: %v", err)
	}
}

// --8<-- [end:lobby-room]
