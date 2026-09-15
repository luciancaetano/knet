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

// --8<-- [start:setname-handler]
// handleSetName lets the client announce (or re-announce, after a
// reconnect — see "Reconnection in practice" in the docs) its display name.
func (s *chatServer) handleSetName(client knet.Client, payload []byte) {
	name := string(payload)
	if name == "" {
		return
	}
	s.names.set(client.ID(), name)
}

// --8<-- [end:setname-handler]

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
// OnCreate above (OnCreate then OnJoin, in that order).
func (r *lobbyRoom) OnJoin(client knet.Client) {
	r.broadcastPresence(CmdUserJoined, client.ID())
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
	payload, _ := json.Marshal(presenceMessage{Name: r.names.get(clientID)})
	if err := r.view.Broadcast(context.Background(), cmd, payload); err != nil {
		log.Printf("broadcast presence failed: %v", err)
	}
}

// --8<-- [end:lobby-room]
