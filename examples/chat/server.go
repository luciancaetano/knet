package main

import (
	"context"
	"encoding/json"
	"log"

	"github.com/luciancaetano/knet"
	"github.com/luciancaetano/knet/roommanager"
)

// --8<-- [start:server-type]
// chatServer holds the shared state for the example: the RoomManager (room
// lifecycle, join/leave, reconnect grace periods — all built in) and the
// display name chosen by each connected client, which RoomManager has no
// concept of (it only knows clientIDs).
type chatServer struct {
	roomManager *roommanager.Manager
	names       *nameStore
}

func newChatServer() *chatServer {
	return &chatServer{names: newNameStore()}
}

// attachRoomManager wires the server up once it exists — RoomManager needs a
// knet.Server to register its handlers on, and that server's config needs
// s.onConnect/s.onDisconnect bound first (see main.go), so this runs after
// ws.New.
func (s *chatServer) attachRoomManager(server knet.Server) {
	s.roomManager = roommanager.New(server, roommanager.Config{})

	// Broadcast a presence message with the joiner's name once they've
	// actually joined the room — the built-in CmdRoomMemberEvent already
	// tells other members a clientID joined/left, this adds the name on top.
	s.roomManager.OnAfterJoin(func(client knet.Client, roomID string) {
		s.broadcastPresence(context.Background(), roomID, CmdUserJoined, s.names.get(client.ID()))
	})
	s.roomManager.OnAfterLeave(func(client knet.Client, roomID string) {
		s.broadcastPresence(context.Background(), roomID, CmdUserLeft, s.names.get(client.ID()))
	})
}

// --8<-- [end:server-type]

// --8<-- [start:connect]
// onConnect just accepts the connection and lets RoomManager track it.
// The client joins the "lobby" room itself via CmdRoomJoin once connected
// (see the web client) — RoomManager doesn't auto-join anyone.
func (s *chatServer) onConnect(client knet.Client) bool {
	log.Printf("client connected: id=%s addr=%s", client.ID(), client.RemoteAddr())
	return s.roomManager.HandleConnect(client)
}

// onDisconnect relays to RoomManager, which handles the voluntary/involuntary
// distinction itself: a voluntary leave removes room membership immediately
// (OnAfterLeave fires, see newChatServer); an involuntary drop keeps
// membership pending for a grace period so a reconnect can resume it.
func (s *chatServer) onDisconnect(client knet.Client, voluntary bool) {
	log.Printf("client disconnected: id=%s addr=%s voluntary=%v", client.ID(), client.RemoteAddr(), voluntary)
	s.roomManager.HandleDisconnect(client, voluntary)
	s.names.delete(client.ID())
}

// onResume lets a reconnecting client automatically resume any pending room
// membership from its grace period, instead of being treated as brand new.
func (s *chatServer) onResume(client knet.Client, previousRooms []string) bool {
	return s.roomManager.HandleResume(client, previousRooms)
}

// --8<-- [end:connect]

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

func (s *chatServer) broadcastPresence(ctx context.Context, roomID string, cmd uint32, name string) {
	r, ok := s.roomManager.Room(roomID)
	if !ok {
		return
	}
	payload, _ := json.Marshal(presenceMessage{Name: name})
	if err := r.Broadcast(ctx, cmd, payload); err != nil {
		log.Printf("broadcast presence failed: %v", err)
	}
}
