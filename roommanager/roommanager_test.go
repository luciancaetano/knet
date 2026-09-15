package roommanager

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/luciancaetano/knet"
	"github.com/luciancaetano/knet/internal/room"
)

// --- test fakes --------------------------------------------------------------

type fakeServer struct {
	handlers map[uint32]knet.HandlerFunc
}

func newFakeServer() *fakeServer { return &fakeServer{handlers: make(map[uint32]knet.HandlerFunc)} }

func (s *fakeServer) Start(ctx context.Context) error { return nil }
func (s *fakeServer) Stop(ctx context.Context) error  { return nil }
func (s *fakeServer) RegisterHandler(ctx context.Context, commandID uint32, handler knet.HandlerFunc) error {
	s.handlers[commandID] = handler
	return nil
}
func (s *fakeServer) RegisterJSONRPCHandler(ctx context.Context, method string, handler knet.JSONRPCHandler) error {
	return nil
}
func (s *fakeServer) BroadcastCommand(ctx context.Context, commandID uint32, payload []byte) error {
	return nil
}

type sentMsg struct {
	cmd     uint32
	payload []byte
}

type fakeClient struct {
	id string

	mu    sync.Mutex
	sent  []sentMsg
	alive bool
}

func newFakeClient(id string) *fakeClient { return &fakeClient{id: id, alive: true} }

func (c *fakeClient) ID() string                                { return c.id }
func (c *fakeClient) RemoteAddr() string                        { return "127.0.0.1:0" }
func (c *fakeClient) Context() context.Context                  { return context.Background() }
func (c *fakeClient) ConnectionPayload() knet.ConnectionPayload { return nil }
func (c *fakeClient) Close(ctx context.Context) error {
	c.mu.Lock()
	c.alive = false
	c.mu.Unlock()
	return nil
}
func (c *fakeClient) CloseWithCode(ctx context.Context, code int, reason string) error {
	return c.Close(ctx)
}
func (c *fakeClient) IsAlive() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.alive
}
func (c *fakeClient) Send(ctx context.Context, command uint32, payload []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sent = append(c.sent, sentMsg{cmd: command, payload: payload})
	return nil
}

func (c *fakeClient) lastOf(cmd uint32) (sentMsg, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i := len(c.sent) - 1; i >= 0; i-- {
		if c.sent[i].cmd == cmd {
			return c.sent[i], true
		}
	}
	return sentMsg{}, false
}

func (c *fakeClient) countOf(cmd uint32) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, m := range c.sent {
		if m.cmd == cmd {
			n++
		}
	}
	return n
}

// --- helpers -------------------------------------------------------------

func join(t *testing.T, srv *fakeServer, client *fakeClient, roomID string) {
	t.Helper()
	req, _ := json.Marshal(RoomJoinRequest{RoomID: roomID})
	srv.handlers[CmdRoomJoin](client, req)
}

func leave(t *testing.T, srv *fakeServer, client *fakeClient, roomID string) {
	t.Helper()
	req, _ := json.Marshal(RoomLeaveRequest{RoomID: roomID})
	srv.handlers[CmdRoomLeave](client, req)
}

func joinTyped(t *testing.T, srv *fakeServer, client *fakeClient, roomID, roomType string) {
	t.Helper()
	req, _ := json.Marshal(RoomJoinRequest{RoomID: roomID, RoomType: roomType})
	srv.handlers[CmdRoomJoin](client, req)
}

// --- tests -----------------------------------------------------------------

func TestJoin_CreatesRoomAndAcks(t *testing.T) {
	srv := newFakeServer()
	m := New(srv, &knet.ConnectHooks{}, Config{})
	a := newFakeClient("a")

	join(t, srv, a, "room-1")

	msg, ok := a.lastOf(CmdRoomJoinAck)
	if !ok {
		t.Fatal("expected join ack")
	}
	var ack RoomJoinAck
	_ = json.Unmarshal(msg.payload, &ack)
	if ack.RoomID != "room-1" || len(ack.Members) != 1 || ack.Members[0] != "a" {
		t.Fatalf("unexpected ack: %+v", ack)
	}
	if rooms := m.Rooms(); len(rooms) != 1 || rooms[0] != "room-1" {
		t.Fatalf("expected room-1 registered, got %v", rooms)
	}
}

func TestJoin_BroadcastsToExistingMembers(t *testing.T) {
	srv := newFakeServer()
	m := New(srv, &knet.ConnectHooks{}, Config{})
	_ = m
	a := newFakeClient("a")
	b := newFakeClient("b")

	join(t, srv, a, "room-1")
	join(t, srv, b, "room-1")

	msg, ok := a.lastOf(CmdRoomMemberEvent)
	if !ok {
		t.Fatal("expected A to receive member-joined event for B")
	}
	var ev RoomMemberEvent
	_ = json.Unmarshal(msg.payload, &ev)
	if ev.Type != MemberJoined || ev.ClientID != "b" {
		t.Fatalf("unexpected event: %+v", ev)
	}

	ackMsg, _ := b.lastOf(CmdRoomJoinAck)
	var ack RoomJoinAck
	_ = json.Unmarshal(ackMsg.payload, &ack)
	if len(ack.Members) != 2 {
		t.Fatalf("expected 2 members in ack, got %v", ack.Members)
	}
}

func TestJoin_Idempotent(t *testing.T) {
	srv := newFakeServer()
	New(srv, &knet.ConnectHooks{}, Config{})
	a := newFakeClient("a")

	join(t, srv, a, "room-1")
	join(t, srv, a, "room-1")

	if n := a.countOf(CmdRoomJoinAck); n != 2 {
		t.Fatalf("expected 2 acks (idempotent re-ack), got %d", n)
	}
	if n := a.countOf(CmdRoomError); n != 0 {
		t.Fatalf("expected no error on duplicate join, got %d", n)
	}
}

func TestLeave_NotAMember(t *testing.T) {
	srv := newFakeServer()
	New(srv, &knet.ConnectHooks{}, Config{})
	a := newFakeClient("a")

	leave(t, srv, a, "room-1")

	msg, ok := a.lastOf(CmdRoomError)
	if !ok {
		t.Fatal("expected CmdRoomError")
	}
	var rerr RoomError
	_ = json.Unmarshal(msg.payload, &rerr)
	if rerr.Code != ErrCodeNotAMember {
		t.Fatalf("expected NOT_A_MEMBER, got %s", rerr.Code)
	}
}

func TestLeave_RemovesAndClosesEmptyRoom(t *testing.T) {
	srv := newFakeServer()
	m := New(srv, &knet.ConnectHooks{}, Config{})
	a := newFakeClient("a")
	b := newFakeClient("b")

	join(t, srv, a, "room-1")
	join(t, srv, b, "room-1")
	leave(t, srv, a, "room-1")

	msg, ok := b.lastOf(CmdRoomMemberEvent)
	if !ok {
		t.Fatal("expected B to see member-left")
	}
	var ev RoomMemberEvent
	_ = json.Unmarshal(msg.payload, &ev)
	if ev.Type != MemberLeft || ev.ClientID != "a" {
		t.Fatalf("unexpected event: %+v", ev)
	}

	leave(t, srv, b, "room-1")
	if rooms := m.Rooms(); len(rooms) != 0 {
		t.Fatalf("expected room-1 to be closed, got %v", rooms)
	}
}

func TestRoomFull(t *testing.T) {
	srv := newFakeServer()
	New(srv, &knet.ConnectHooks{}, Config{MaxMembersPerRoom: 1})
	a := newFakeClient("a")
	b := newFakeClient("b")

	join(t, srv, a, "room-1")
	join(t, srv, b, "room-1")

	msg, ok := b.lastOf(CmdRoomError)
	if !ok {
		t.Fatal("expected CmdRoomError for full room")
	}
	var rerr RoomError
	_ = json.Unmarshal(msg.payload, &rerr)
	if rerr.Code != ErrCodeRoomFull {
		t.Fatalf("expected ROOM_FULL, got %s", rerr.Code)
	}
}

func TestOnBeforeJoin_Rejection(t *testing.T) {
	srv := newFakeServer()
	m := New(srv, &knet.ConnectHooks{}, Config{})
	m.OnBeforeJoin(func(client knet.Client, roomID string) error {
		return errDenied
	})
	a := newFakeClient("a")

	join(t, srv, a, "room-1")

	msg, ok := a.lastOf(CmdRoomError)
	if !ok {
		t.Fatal("expected CmdRoomError")
	}
	var rerr RoomError
	_ = json.Unmarshal(msg.payload, &rerr)
	if rerr.Code != ErrCodeJoinRejected {
		t.Fatalf("expected JOIN_REJECTED, got %s", rerr.Code)
	}
	if rooms := m.Rooms(); len(rooms) != 0 {
		t.Fatalf("room should not be created on rejection, got %v", rooms)
	}
}

var errDenied = &testErr{"denied"}

type testErr struct{ msg string }

func (e *testErr) Error() string { return e.msg }

func TestDisconnect_Voluntary_RemovesImmediately(t *testing.T) {
	srv := newFakeServer()
	m := New(srv, &knet.ConnectHooks{}, Config{})
	a := newFakeClient("a")
	b := newFakeClient("b")
	join(t, srv, a, "room-1")
	join(t, srv, b, "room-1")

	m.HandleDisconnect(a, true)

	msg, ok := b.lastOf(CmdRoomMemberEvent)
	if !ok {
		t.Fatal("expected member event")
	}
	var ev RoomMemberEvent
	_ = json.Unmarshal(msg.payload, &ev)
	if ev.Type != MemberLeft {
		t.Fatalf("expected left on voluntary disconnect, got %s", ev.Type)
	}
	if rooms := m.RoomsOf("a"); len(rooms) != 0 {
		t.Fatalf("expected a removed from rooms, got %v", rooms)
	}
}

func TestDisconnect_Involuntary_GraceThenResume(t *testing.T) {
	srv := newFakeServer()
	m := New(srv, &knet.ConnectHooks{}, Config{GraceTTL: 200 * time.Millisecond})
	a := newFakeClient("a")
	b := newFakeClient("b")
	join(t, srv, a, "room-1")
	join(t, srv, b, "room-1")

	m.HandleDisconnect(a, false)

	msg, ok := b.lastOf(CmdRoomMemberEvent)
	if !ok {
		t.Fatal("expected disconnected event")
	}
	var ev RoomMemberEvent
	_ = json.Unmarshal(msg.payload, &ev)
	if ev.Type != MemberDisconnected {
		t.Fatalf("expected disconnected, got %s", ev.Type)
	}

	if rooms := m.RoomsOf("a"); len(rooms) != 1 || rooms[0] != "room-1" {
		t.Fatalf("expected a still pending in room-1, got %v", rooms)
	}

	// Reconnect within grace period.
	a2 := newFakeClient("a") // resumed client reuses the same ID
	ok2 := m.HandleResume(a2, nil)
	if !ok2 {
		t.Fatal("expected resume to be accepted")
	}

	msg, ok = b.lastOf(CmdRoomMemberEvent)
	if !ok {
		t.Fatal("expected reconnected event")
	}
	_ = json.Unmarshal(msg.payload, &ev)
	if ev.Type != MemberReconnected {
		t.Fatalf("expected reconnected, got %s", ev.Type)
	}

	syncMsg, ok := a2.lastOf(CmdRoomResumeSync)
	if !ok {
		t.Fatal("expected resume sync sent to resumed client")
	}
	var sync RoomResumeSync
	_ = json.Unmarshal(syncMsg.payload, &sync)
	if len(sync.Rooms) != 1 || sync.Rooms[0].RoomID != "room-1" {
		t.Fatalf("unexpected resume sync: %+v", sync)
	}
}

func TestDisconnect_Involuntary_GraceExpires(t *testing.T) {
	srv := newFakeServer()
	m := New(srv, &knet.ConnectHooks{}, Config{GraceTTL: 50 * time.Millisecond})
	a := newFakeClient("a")
	b := newFakeClient("b")
	join(t, srv, a, "room-1")
	join(t, srv, b, "room-1")

	m.HandleDisconnect(a, false)

	deadline := time.After(2 * time.Second)
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for len(m.RoomsOf("a")) != 0 {
		select {
		case <-tick.C:
		case <-deadline:
			t.Fatal("grace period never expired")
		}
	}

	// b should have seen exactly one "left" event (not a second "disconnected").
	msg, ok := b.lastOf(CmdRoomMemberEvent)
	if !ok {
		t.Fatal("expected member event")
	}
	var ev RoomMemberEvent
	_ = json.Unmarshal(msg.payload, &ev)
	if ev.Type != MemberLeft {
		t.Fatalf("expected left after grace expiry, got %s", ev.Type)
	}
}

func sendMessage(t *testing.T, srv *fakeServer, client *fakeClient, roomID, msgType, data string) {
	t.Helper()
	req, _ := json.Marshal(RoomMessageRequest{RoomID: roomID, Type: msgType, Data: data})
	srv.handlers[CmdRoomMessage](client, req)
}

func TestMessage_BroadcastsToOtherMembersOnly(t *testing.T) {
	srv := newFakeServer()
	New(srv, &knet.ConnectHooks{}, Config{})
	a := newFakeClient("a")
	b := newFakeClient("b")
	join(t, srv, a, "room-1")
	join(t, srv, b, "room-1")

	sendMessage(t, srv, a, "room-1", "chat", "hi")

	msg, ok := b.lastOf(CmdRoomMessage)
	if !ok {
		t.Fatal("expected b to receive the message")
	}
	var ev RoomMessageEvent
	_ = json.Unmarshal(msg.payload, &ev)
	if ev.RoomID != "room-1" || ev.SenderID != "a" || ev.Type != "chat" || ev.Data != "hi" {
		t.Fatalf("unexpected event: %+v", ev)
	}
	if n := a.countOf(CmdRoomMessage); n != 0 {
		t.Fatalf("sender should not receive its own message, got %d", n)
	}
}

func TestMessage_NotAMember(t *testing.T) {
	srv := newFakeServer()
	New(srv, &knet.ConnectHooks{}, Config{})
	a := newFakeClient("a")

	sendMessage(t, srv, a, "room-1", "chat", "hi")

	msg, ok := a.lastOf(CmdRoomError)
	if !ok {
		t.Fatal("expected CmdRoomError")
	}
	var rerr RoomError
	_ = json.Unmarshal(msg.payload, &rerr)
	if rerr.Code != ErrCodeNotAMember {
		t.Fatalf("expected NOT_A_MEMBER, got %s", rerr.Code)
	}
}

func TestMessage_OnRoomMessageRejection(t *testing.T) {
	srv := newFakeServer()
	m := New(srv, &knet.ConnectHooks{}, Config{})
	m.OnRoomMessage(func(client knet.Client, roomID, msgType, data string) error {
		return errDenied
	})
	a := newFakeClient("a")
	b := newFakeClient("b")
	join(t, srv, a, "room-1")
	join(t, srv, b, "room-1")

	sendMessage(t, srv, a, "room-1", "chat", "hi")

	msg, ok := a.lastOf(CmdRoomError)
	if !ok {
		t.Fatal("expected CmdRoomError")
	}
	var rerr RoomError
	_ = json.Unmarshal(msg.payload, &rerr)
	if rerr.Code != ErrCodeMessageRejected {
		t.Fatalf("expected MESSAGE_REJECTED, got %s", rerr.Code)
	}
	if n := b.countOf(CmdRoomMessage); n != 0 {
		t.Fatalf("rejected message should not be broadcast, got %d", n)
	}
}

func TestMultiRoomMembership(t *testing.T) {
	srv := newFakeServer()
	m := New(srv, &knet.ConnectHooks{}, Config{})
	a := newFakeClient("a")

	join(t, srv, a, "room-1")
	join(t, srv, a, "room-2")

	rooms := m.RoomsOf("a")
	if len(rooms) != 2 {
		t.Fatalf("expected membership in 2 rooms, got %v", rooms)
	}
}

func TestHooks_JoinLeaveOrder(t *testing.T) {
	srv := newFakeServer()
	m := New(srv, &knet.ConnectHooks{}, Config{})

	var events []string
	m.OnRoomCreated(func(roomID string) { events = append(events, "created:"+roomID) })
	m.OnAfterJoin(func(client knet.Client, roomID string) { events = append(events, "afterJoin:"+client.ID()) })
	m.OnAfterLeave(func(client knet.Client, roomID string) { events = append(events, "afterLeave:"+client.ID()) })
	m.OnRoomClosed(func(roomID string) { events = append(events, "closed:"+roomID) })

	a := newFakeClient("a")
	join(t, srv, a, "room-1")
	leave(t, srv, a, "room-1")

	want := []string{"created:room-1", "afterJoin:a", "afterLeave:a", "closed:room-1"}
	if len(events) != len(want) {
		t.Fatalf("got %v, want %v", events, want)
	}
	for i := range want {
		if events[i] != want[i] {
			t.Fatalf("got %v, want %v", events, want)
		}
	}
}

// --- RoomHandler (Define) ---------------------------------------------------

type fakeHandler struct {
	events *[]string
}

func (h *fakeHandler) OnCreate(v room.View) { *h.events = append(*h.events, "create:"+v.ID()) }
func (h *fakeHandler) OnJoin(c knet.Client, _ json.RawMessage) {
	*h.events = append(*h.events, "join:"+c.ID())
}
func (h *fakeHandler) OnLeave(c knet.Client) {
	*h.events = append(*h.events, "leave:"+c.ID())
}
func (h *fakeHandler) OnDispose() { *h.events = append(*h.events, "dispose") }

func TestDefine_LifecycleCalledOncePerRoom(t *testing.T) {
	srv := newFakeServer()
	m := New(srv, &knet.ConnectHooks{}, Config{})

	var events []string
	m.Define("match", func() RoomHandler { return &fakeHandler{events: &events} })

	a := newFakeClient("a")
	b := newFakeClient("b")

	joinTyped(t, srv, a, "match-1", "match")
	joinTyped(t, srv, b, "match-1", "match")
	leave(t, srv, a, "match-1")
	leave(t, srv, b, "match-1")

	want := []string{"create:match-1", "join:a", "join:b", "leave:a", "leave:b", "dispose"}
	if len(events) != len(want) {
		t.Fatalf("got %v, want %v", events, want)
	}
	for i := range want {
		if events[i] != want[i] {
			t.Fatalf("got %v, want %v", events, want)
		}
	}
}

func TestDefine_UntypedRoomHasNoHandler(t *testing.T) {
	srv := newFakeServer()
	m := New(srv, &knet.ConnectHooks{}, Config{})

	called := false
	m.Define("match", func() RoomHandler {
		called = true
		return &fakeHandler{events: &[]string{}}
	})

	a := newFakeClient("a")
	join(t, srv, a, "room-1") // no RoomType

	if called {
		t.Fatal("factory should not run for a join without RoomType")
	}
}

func TestDefine_CoexistsWithGlobalHooks(t *testing.T) {
	srv := newFakeServer()
	m := New(srv, &knet.ConnectHooks{}, Config{})

	var globalEvents []string
	m.OnAfterJoin(func(client knet.Client, roomID string) {
		globalEvents = append(globalEvents, "global:"+client.ID())
	})

	var handlerEvents []string
	m.Define("match", func() RoomHandler { return &fakeHandler{events: &handlerEvents} })

	a := newFakeClient("a")
	joinTyped(t, srv, a, "match-1", "match")

	if len(globalEvents) != 1 || globalEvents[0] != "global:a" {
		t.Fatalf("expected global hook to still fire, got %v", globalEvents)
	}
	if len(handlerEvents) != 2 || handlerEvents[0] != "create:match-1" || handlerEvents[1] != "join:a" {
		t.Fatalf("expected handler lifecycle to also fire, got %v", handlerEvents)
	}
}
