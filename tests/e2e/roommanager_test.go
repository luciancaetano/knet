package e2e_test

import (
	"context"
	"encoding/json"
	"net/url"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/luciancaetano/knet"
	"github.com/luciancaetano/knet/internal/protocol"
	"github.com/luciancaetano/knet/roommanager"
	"github.com/luciancaetano/knet/ws"
)

// readCmd reads frames off conn until one with the wanted command ID
// arrives (or deadline), decoding and returning its payload. Other command
// IDs seen along the way are skipped, since events for other rooms/clients
// may interleave with the one under test.
func readCmd(t *testing.T, conn *websocket.Conn, want uint32, timeout time.Duration) []byte {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			t.Fatalf("timed out waiting for command 0x%x", want)
		}
		_ = conn.SetReadDeadline(time.Now().Add(remaining))
		_, data, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("read error while waiting for 0x%x: %v", want, err)
		}
		_, cmd, payload, err := protocol.Decode(data)
		if err != nil {
			t.Fatalf("decode error: %v", err)
		}
		if cmd == want {
			return payload
		}
	}
}

func sendJoin(t *testing.T, conn *websocket.Conn, roomID string) {
	t.Helper()
	payload, _ := json.Marshal(roommanager.RoomJoinRequest{RoomID: roomID})
	encoded, _ := protocol.Encode(roommanager.CmdRoomJoin, payload)
	if err := conn.WriteMessage(websocket.BinaryMessage, encoded); err != nil {
		t.Fatalf("failed to send join: %v", err)
	}
}

// TestRoomManagerE2E wires roommanager into a real websocket server per the
// spec's CON-001 manual-wiring pattern and exercises AC-001, AC-002, AC-004,
// AC-005: join creates a room and acks with the member list, a second join
// broadcasts member-joined to the existing member, an involuntary disconnect
// broadcasts member-disconnected and keeps membership pending, and a resume
// within the grace TTL automatically rejoins with member-reconnected +
// CmdRoomResumeSync.
func TestRoomManagerE2E(t *testing.T) {
	t.Parallel()

	store := newTestSessionStore()

	// lobby is assigned below, once the real server exists (roommanager.New
	// needs it to register handlers). It self-registers its connect/
	// disconnect tracking onto hooks, alongside this test's own session-store
	// priming listener.
	var lobby *roommanager.Manager

	hooks := &knet.ConnectHooks{}
	hooks.OnConnect(func(c knet.Client) bool {
		// Prerequisite for resume support: prime the session store so
		// the server's built-in grace-period extension (on involuntary
		// disconnect) has an entry to extend. See CON-003/REQ-009.
		store.Put(c.ID(), nil, 3*time.Second)
		return true
	})

	cfg := ws.NewConfig(":18090", ws.DefaultRateLimitConfig(), ws.AllOrigins(),
		hooks.DispatchConnect, hooks.DispatchDisconnect)
	cfg.SessionStore = store
	cfg.SessionGraceTTL = 3 * time.Second
	cfg.OnResume = func(c knet.Client, previousRooms []string) bool {
		return lobby.HandleResume(c, previousRooms)
	}

	server := ws.New(cfg)
	// New wires HandleConnect/HandleDisconnect onto hooks itself; OnResume
	// still needs manual wiring above (single-slot ServerConfig field).
	lobby = roommanager.New(server, hooks, roommanager.Config{GraceTTL: 3 * time.Second})

	ctx := context.Background()
	if err := server.Start(ctx); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Stop(stopCtx)
	}()

	time.Sleep(200 * time.Millisecond)

	// --- A connects and joins "lobby" (AC-001) ---
	connA, _, err := newDialer().Dial("ws://localhost:18090/ws", nil)
	if err != nil {
		t.Fatalf("A dial failed: %v", err)
	}
	defer func() { _ = connA.Close() }()

	sendJoin(t, connA, "lobby")
	ackPayload := readCmd(t, connA, roommanager.CmdRoomJoinAck, 5*time.Second)
	var ackA roommanager.RoomJoinAck
	if err := json.Unmarshal(ackPayload, &ackA); err != nil {
		t.Fatalf("failed to decode ack: %v", err)
	}
	if ackA.RoomID != "lobby" || len(ackA.Members) != 1 {
		t.Fatalf("unexpected AC-001 ack: %+v", ackA)
	}
	aID := ackA.Members[0]

	// --- B connects and joins "lobby"; A sees member-joined (AC-002) ---
	connB, _, err := newDialer().Dial("ws://localhost:18090/ws", nil)
	if err != nil {
		t.Fatalf("B dial failed: %v", err)
	}
	defer func() { _ = connB.Close() }()

	sendJoin(t, connB, "lobby")

	evPayload := readCmd(t, connA, roommanager.CmdRoomMemberEvent, 5*time.Second)
	var ev roommanager.RoomMemberEvent
	_ = json.Unmarshal(evPayload, &ev)
	if ev.Type != roommanager.MemberJoined {
		t.Fatalf("AC-002: expected joined event on A, got %+v", ev)
	}

	ackBPayload := readCmd(t, connB, roommanager.CmdRoomJoinAck, 5*time.Second)
	var ackB roommanager.RoomJoinAck
	_ = json.Unmarshal(ackBPayload, &ackB)
	if len(ackB.Members) != 2 {
		t.Fatalf("AC-002: expected 2 members in B's ack, got %v", ackB.Members)
	}

	// --- A disconnects involuntarily (garbage frame -> protocol error close)
	// B sees member-disconnected (AC-004) ---
	_ = connA.WriteMessage(websocket.BinaryMessage, []byte{0x00, 0x01, 0x02}) // malformed frame

	evPayload = readCmd(t, connB, roommanager.CmdRoomMemberEvent, 5*time.Second)
	_ = json.Unmarshal(evPayload, &ev)
	if ev.Type != roommanager.MemberDisconnected || ev.ClientID != aID {
		t.Fatalf("AC-004: expected disconnected event for %s, got %+v", aID, ev)
	}
	if rooms := lobby.RoomsOf(aID); len(rooms) != 1 {
		t.Fatalf("AC-004: expected A still pending in 1 room, got %v", rooms)
	}

	// --- A reconnects with its session ID within grace TTL; auto-rejoined
	// (AC-005) ---
	u := url.URL{Scheme: "ws", Host: "localhost:18090", Path: "/ws", RawQuery: "session=" + aID}
	connA2, _, err := newDialer().Dial(u.String(), nil)
	if err != nil {
		t.Fatalf("A resume dial failed: %v", err)
	}
	defer func() { _ = connA2.Close() }()

	syncPayload := readCmd(t, connA2, roommanager.CmdRoomResumeSync, 5*time.Second)
	var sync roommanager.RoomResumeSync
	_ = json.Unmarshal(syncPayload, &sync)
	if len(sync.Rooms) != 1 || sync.Rooms[0].RoomID != "lobby" {
		t.Fatalf("AC-005: unexpected resume sync: %+v", sync)
	}

	evPayload = readCmd(t, connB, roommanager.CmdRoomMemberEvent, 5*time.Second)
	_ = json.Unmarshal(evPayload, &ev)
	if ev.Type != roommanager.MemberReconnected || ev.ClientID != aID {
		t.Fatalf("AC-005: expected reconnected event for %s, got %+v", aID, ev)
	}
}
