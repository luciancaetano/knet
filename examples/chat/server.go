package main

import (
	"context"
	"encoding/json"
	"log"

	"github.com/luciancaetano/knet"
	"github.com/luciancaetano/knet/room"
)

// --8<-- [start:server-type]
// chatServer holds the shared state for the "lobby" room: the set of
// connected clients and the display name chosen by each of them.
type chatServer struct {
	lobby room.Room
	names *nameStore
}

func newChatServer() *chatServer {
	return &chatServer{
		lobby: room.New("lobby"),
		names: newNameStore(),
	}
}

// --8<-- [end:server-type]

// --8<-- [start:connect]
// onConnect adds the socket to the lobby and accepts the connection.
// The client has no display name yet — it sends one via CmdSetName right
// after connecting (see the web client), so we don't broadcast "joined" here.
func (s *chatServer) onConnect(client knet.Client) bool {
	s.lobby.Add(client)
	log.Printf("client connected: id=%s addr=%s", client.ID(), client.RemoteAddr())
	return true // true = accept the connection
}

// onDisconnect is where knet tells us HOW the client left.
//
//	voluntary == true  -> the client closed the connection normally (a real
//	                       close frame — e.g. the user clicked "Leave").
//	voluntary == false -> the connection dropped without a clean close
//	                       (network failure, tab killed, read timeout).
//
// No application-level "I'm leaving" message is needed: knet already makes
// this distinction for us, so we just relay it as the right presence event.
func (s *chatServer) onDisconnect(client knet.Client, voluntary bool) {
	name := s.names.get(client.ID())
	s.names.delete(client.ID())
	s.lobby.Remove(client.ID())

	if voluntary {
		log.Printf("client left: id=%s addr=%s name=%q", client.ID(), client.RemoteAddr(), name)
		s.broadcastPresence(context.Background(), CmdUserLeft, name)
	} else {
		log.Printf("client disconnected: id=%s addr=%s name=%q", client.ID(), client.RemoteAddr(), name)
		s.broadcastPresence(context.Background(), CmdUserDisconnected, name)
	}
}

// --8<-- [end:connect]

// --8<-- [start:setname-handler]
// handleSetName lets the client announce (or re-announce, after a
// reconnect — see "Reconnection in practice" in the docs) its display name.
// We broadcast "joined" here, once we actually have a name to show.
func (s *chatServer) handleSetName(client knet.Client, payload []byte) {
	name := string(payload)
	if name == "" {
		return
	}
	s.names.set(client.ID(), name)
	s.broadcastPresence(context.Background(), CmdUserJoined, name)
}

// --8<-- [end:setname-handler]

// --8<-- [start:chat-handler]
// handleChat relays a chat message to everyone in the room, tagged with the
// sender's name so clients can render "name: text".
func (s *chatServer) handleChat(client knet.Client, payload []byte) {
	ctx := context.Background()
	msg := chatMessage{From: s.names.get(client.ID()), Text: string(payload)}

	out, err := json.Marshal(msg)
	if err != nil {
		log.Printf("encode chat message: %v", err)
		return
	}
	if err := s.lobby.Broadcast(ctx, CmdChat, out); err != nil {
		log.Printf("broadcast chat message: %v", err)
	}
}

// --8<-- [end:chat-handler]

func (s *chatServer) broadcastPresence(ctx context.Context, cmd uint32, name string) {
	payload, _ := json.Marshal(presenceMessage{Name: name})
	if err := s.lobby.Broadcast(ctx, cmd, payload); err != nil {
		log.Printf("broadcast presence failed: %v", err)
	}
}
