// Package roommanager is an optional, opt-in layer on top of knet's
// room.Room primitive. It provides a named/discoverable multi-room registry,
// reserved join/leave commands, automatic create-on-first-join /
// close-on-empty room lifecycle, and grace-period disconnect/reconnect
// handling for room membership.
//
// RoomManager never modifies knet core: it is constructed explicitly by the
// application and wired into ServerConfig's OnConnect/OnClientDisconnect/
// OnResume hooks by hand (see HandleConnect, HandleDisconnect, HandleResume)
// because those hooks hold a single function reference each. An application
// that never calls New has zero behavior change.
package roommanager

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/luciancaetano/knet"
	"github.com/luciancaetano/knet/room"
)

// defaultGraceTTL matches knet's own default SessionGraceTTL
// (internal/websocket.Server), used when Config.GraceTTL is 0.
const defaultGraceTTL = 30 * time.Second

// Config configures a Manager.
type Config struct {
	// GraceTTL is how long a room membership stays pending after an
	// involuntary disconnect, giving the client a window to reconnect and
	// resume. 0 = defaultGraceTTL (30s), matching knet's own default
	// SessionGraceTTL.
	GraceTTL time.Duration

	// MaxMembersPerRoom caps how many clients may join a single room.
	// 0 = unlimited.
	MaxMembersPerRoom int
}

// pendingMember tracks a disconnected-but-not-yet-finalized client's room
// membership during its grace period.
type pendingMember struct {
	rooms []string
	timer *time.Timer
}

// Manager owns a registry of rooms, per-client membership, and the
// disconnect/reconnect grace-period state machine. It is an ordinary struct
// returned by New — not a package-level singleton (PAT-001).
type Manager struct {
	server knet.Server
	cfg    Config

	mu         sync.Mutex
	rooms      map[string]room.Room       // roomID -> room
	membership map[string]map[string]bool // clientID -> set of active roomIDs
	pending    map[string]*pendingMember  // clientID -> grace-period state

	hookMu        sync.RWMutex
	onBeforeJoin  func(client knet.Client, roomID string) error
	onAfterJoin   func(client knet.Client, roomID string)
	onBeforeLeave func(client knet.Client, roomID string)
	onAfterLeave  func(client knet.Client, roomID string)
	onRoomCreated func(roomID string)
	onRoomClosed  func(roomID string)
	onRoomMessage func(client knet.Client, roomID, msgType, data string) error
}

// New creates a Manager attached to server and registers its reserved
// join/leave command handlers. It does not touch server's connect/disconnect
// hooks — wire HandleConnect/HandleDisconnect/HandleResume into your own
// ServerConfig callbacks explicitly (see package doc and spec §9).
func New(server knet.Server, cfg Config) *Manager {
	if cfg.GraceTTL <= 0 {
		cfg.GraceTTL = defaultGraceTTL
	}

	m := &Manager{
		server:     server,
		cfg:        cfg,
		rooms:      make(map[string]room.Room),
		membership: make(map[string]map[string]bool),
		pending:    make(map[string]*pendingMember),
	}

	ctx := context.Background()
	server.RegisterHandler(ctx, CmdRoomJoin, m.handleJoin)       //nolint:errcheck
	server.RegisterHandler(ctx, CmdRoomLeave, m.handleLeave)     //nolint:errcheck
	server.RegisterHandler(ctx, CmdRoomMessage, m.handleMessage) //nolint:errcheck

	return m
}

// --- Connection lifecycle wiring (CON-001) ---------------------------------

// HandleConnect should be called from the application's OnConnectFn. It
// currently performs no work of its own but is part of the public wiring
// contract (REQ-012) and future-proofs per-connect bookkeeping.
func (m *Manager) HandleConnect(_ knet.Client) bool {
	return true
}

// HandleDisconnect should be called from the application's
// OnClientDisconnectFn. On a voluntary disconnect the client is removed from
// every room it was a member of (member-left broadcast to each). On an
// involuntary disconnect, membership is kept pending for GraceTTL
// (member-disconnected broadcast instead) so a resuming client can be
// rejoined automatically by HandleResume.
func (m *Manager) HandleDisconnect(client knet.Client, voluntary bool) {
	clientID := client.ID()

	m.mu.Lock()
	roomIDs := setToSlice(m.membership[clientID])
	delete(m.membership, clientID)
	m.mu.Unlock()

	if len(roomIDs) == 0 {
		return
	}

	if voluntary {
		for _, roomID := range roomIDs {
			m.finalizeLeave(clientID, roomID)
		}
		return
	}

	for _, roomID := range roomIDs {
		m.broadcastMemberEvent(roomID, clientID, MemberDisconnected)
	}

	timer := time.AfterFunc(m.cfg.GraceTTL, func() { m.expireGrace(clientID) })
	m.mu.Lock()
	m.pending[clientID] = &pendingMember{rooms: roomIDs, timer: timer}
	m.mu.Unlock()
}

// HandleResume should be called from the application's OnResume callback.
// If the client (identified by its resumed client ID) has pending grace-
// period room memberships, it is rejoined to each automatically and
// member-reconnected is broadcast to each room's other members; the client
// itself receives a CmdRoomResumeSync listing its rejoined rooms. Returns
// true to accept the resume (RoomManager never rejects a resume itself).
func (m *Manager) HandleResume(client knet.Client, previousRooms []string) bool {
	clientID := client.ID()

	m.mu.Lock()
	p, ok := m.pending[clientID]
	if ok {
		p.timer.Stop()
		delete(m.pending, clientID)
	}
	m.mu.Unlock()

	roomIDs := previousRooms
	if ok {
		roomIDs = p.rooms
	}
	if len(roomIDs) == 0 {
		return true
	}

	resyncPayload := RoomResumeSync{Rooms: make([]RoomResumeSyncEntry, 0, len(roomIDs))}
	for _, roomID := range roomIDs {
		m.mu.Lock()
		rm, exists := m.rooms[roomID]
		if !exists {
			m.mu.Unlock()
			continue
		}
		rm.Add(client)
		if m.membership[clientID] == nil {
			m.membership[clientID] = make(map[string]bool)
		}
		m.membership[clientID][roomID] = true
		members := memberIDs(rm)
		m.mu.Unlock()

		m.broadcastMemberEvent(roomID, clientID, MemberReconnected)
		resyncPayload.Rooms = append(resyncPayload.Rooms, RoomResumeSyncEntry{RoomID: roomID, Members: members})
	}

	m.send(client, CmdRoomResumeSync, resyncPayload)
	return true
}

// expireGrace finalizes the removal of a client whose grace period elapsed
// without a resume (REQ-011): drops it from every pending room and
// broadcasts member-left (not a second member-disconnected).
func (m *Manager) expireGrace(clientID string) {
	m.mu.Lock()
	p, ok := m.pending[clientID]
	if !ok {
		m.mu.Unlock()
		return
	}
	delete(m.pending, clientID)
	m.mu.Unlock()

	for _, roomID := range p.rooms {
		m.finalizeLeave(clientID, roomID)
	}
}

// --- Query API (REQ-013) ----------------------------------------------------

// Room returns the room with the given ID, if it currently exists.
func (m *Manager) Room(roomID string) (room.Room, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rm, ok := m.rooms[roomID]
	return rm, ok
}

// RoomsOf returns the room IDs clientID currently belongs to, including
// rooms it is pending removal from during its grace period.
func (m *Manager) RoomsOf(clientID string) []string {
	m.mu.Lock()
	defer m.mu.Unlock()

	seen := make(map[string]bool)
	out := make([]string, 0)
	for roomID := range m.membership[clientID] {
		if !seen[roomID] {
			seen[roomID] = true
			out = append(out, roomID)
		}
	}
	if p, ok := m.pending[clientID]; ok {
		for _, roomID := range p.rooms {
			if !seen[roomID] {
				seen[roomID] = true
				out = append(out, roomID)
			}
		}
	}
	return out
}

// Rooms returns the IDs of every currently registered (non-empty) room.
func (m *Manager) Rooms() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, 0, len(m.rooms))
	for id := range m.rooms {
		out = append(out, id)
	}
	return out
}

// --- Hooks (REQ-012) --------------------------------------------------------

// OnBeforeJoin registers fn, called before a join is applied. A non-nil
// error rejects the join: the client receives CmdRoomError and the room is
// not created/modified.
func (m *Manager) OnBeforeJoin(fn func(client knet.Client, roomID string) error) {
	m.hookMu.Lock()
	m.onBeforeJoin = fn
	m.hookMu.Unlock()
}

// OnAfterJoin registers fn, called after a join succeeds.
func (m *Manager) OnAfterJoin(fn func(client knet.Client, roomID string)) {
	m.hookMu.Lock()
	m.onAfterJoin = fn
	m.hookMu.Unlock()
}

// OnBeforeLeave registers fn, called before a leave (explicit or via
// disconnect/grace-expiry) is applied.
func (m *Manager) OnBeforeLeave(fn func(client knet.Client, roomID string)) {
	m.hookMu.Lock()
	m.onBeforeLeave = fn
	m.hookMu.Unlock()
}

// OnAfterLeave registers fn, called after a leave is applied.
func (m *Manager) OnAfterLeave(fn func(client knet.Client, roomID string)) {
	m.hookMu.Lock()
	m.onAfterLeave = fn
	m.hookMu.Unlock()
}

// OnRoomCreated registers fn, called when a room is created (first join).
func (m *Manager) OnRoomCreated(fn func(roomID string)) {
	m.hookMu.Lock()
	m.onRoomCreated = fn
	m.hookMu.Unlock()
}

// OnRoomClosed registers fn, called when a room is closed (membership
// reaches zero).
func (m *Manager) OnRoomClosed(fn func(roomID string)) {
	m.hookMu.Lock()
	m.onRoomClosed = fn
	m.hookMu.Unlock()
}

// OnRoomMessage registers fn, called for every CmdRoomMessage received from
// a room member before it is broadcast to the room — this is the "onCommand,
// scoped to a room" hook: use it to observe, validate, or transform
// client-sent room messages (e.g. persist a chat line, rate-limit, moderate).
// A non-nil error rejects the message: the sender receives CmdRoomError and
// nothing is broadcast.
func (m *Manager) OnRoomMessage(fn func(client knet.Client, roomID, msgType, data string) error) {
	m.hookMu.Lock()
	m.onRoomMessage = fn
	m.hookMu.Unlock()
}

// --- Command handlers --------------------------------------------------------

func (m *Manager) handleJoin(client knet.Client, payload []byte) {
	var req RoomJoinRequest
	if err := json.Unmarshal(payload, &req); err != nil || req.RoomID == "" {
		m.sendError(client, "", ErrCodeInvalidPayload, "invalid join request")
		return
	}

	clientID := client.ID()

	m.mu.Lock()
	if m.membership[clientID][req.RoomID] {
		// Idempotent: already a member, re-ack with the current list.
		rm := m.rooms[req.RoomID]
		members := memberIDs(rm)
		m.mu.Unlock()
		m.send(client, CmdRoomJoinAck, RoomJoinAck{RoomID: req.RoomID, Members: members})
		return
	}
	m.mu.Unlock()

	if beforeJoin := m.getBeforeJoin(); beforeJoin != nil {
		if err := beforeJoin(client, req.RoomID); err != nil {
			m.sendError(client, req.RoomID, ErrCodeJoinRejected, err.Error())
			return
		}
	}

	m.mu.Lock()
	rm, existed := m.rooms[req.RoomID]
	if !existed {
		rm = room.New(req.RoomID)
	}

	if m.cfg.MaxMembersPerRoom > 0 && rm.Size() >= m.cfg.MaxMembersPerRoom {
		m.mu.Unlock()
		m.sendError(client, req.RoomID, ErrCodeRoomFull, "room is full")
		return
	}

	if !existed {
		m.rooms[req.RoomID] = rm
	}
	rm.Add(client)
	if m.membership[clientID] == nil {
		m.membership[clientID] = make(map[string]bool)
	}
	m.membership[clientID][req.RoomID] = true
	members := memberIDs(rm)
	m.mu.Unlock()

	if !existed {
		if fn := m.getRoomCreated(); fn != nil {
			fn(req.RoomID)
		}
	}
	if fn := m.getAfterJoin(); fn != nil {
		fn(client, req.RoomID)
	}

	m.broadcastMemberEvent(req.RoomID, clientID, MemberJoined)
	m.send(client, CmdRoomJoinAck, RoomJoinAck{RoomID: req.RoomID, Members: members})
}

func (m *Manager) handleLeave(client knet.Client, payload []byte) {
	var req RoomLeaveRequest
	if err := json.Unmarshal(payload, &req); err != nil || req.RoomID == "" {
		m.sendError(client, "", ErrCodeInvalidPayload, "invalid leave request")
		return
	}

	clientID := client.ID()

	m.mu.Lock()
	isMember := m.membership[clientID][req.RoomID]
	m.mu.Unlock()
	if !isMember {
		m.sendError(client, req.RoomID, ErrCodeNotAMember, "not a member of this room")
		return
	}

	m.finalizeLeave(clientID, req.RoomID)
	m.send(client, CmdRoomLeaveAck, RoomLeaveAck{RoomID: req.RoomID})
}

// handleMessage validates that the sender is a member of RoomID, runs the
// OnRoomMessage hook (if any), and broadcasts CmdRoomMessage to the room's
// other members. This is the general-purpose "send something to the room"
// path — unlike join/leave it carries no server-side meaning of its own, it
// is just member-scoped fan-out with an app-observable hook.
func (m *Manager) handleMessage(client knet.Client, payload []byte) {
	var req RoomMessageRequest
	if err := json.Unmarshal(payload, &req); err != nil || req.RoomID == "" || req.Type == "" {
		m.sendError(client, "", ErrCodeInvalidPayload, "invalid message request")
		return
	}

	clientID := client.ID()

	m.mu.Lock()
	isMember := m.membership[clientID][req.RoomID]
	m.mu.Unlock()
	if !isMember {
		m.sendError(client, req.RoomID, ErrCodeNotAMember, "not a member of this room")
		return
	}

	if onMessage := m.getRoomMessage(); onMessage != nil {
		if err := onMessage(client, req.RoomID, req.Type, req.Data); err != nil {
			m.sendError(client, req.RoomID, ErrCodeMessageRejected, err.Error())
			return
		}
	}

	m.mu.Lock()
	rm, ok := m.rooms[req.RoomID]
	m.mu.Unlock()
	if !ok {
		return
	}

	payloadOut, err := json.Marshal(RoomMessageEvent{
		RoomID: req.RoomID, SenderID: clientID, Type: req.Type, Data: req.Data,
	})
	if err != nil {
		return
	}
	rm.BroadcastExcept(context.Background(), clientID, CmdRoomMessage, payloadOut) //nolint:errcheck
}

// finalizeLeave removes clientID from roomID (if present), broadcasts
// member-left to the remaining members, runs the before/after-leave hooks,
// and closes the room if it is now empty. Shared by explicit leave,
// voluntary disconnect, and grace-period expiry.
func (m *Manager) finalizeLeave(clientID, roomID string) {
	client, hadClient := m.roomMember(roomID, clientID)

	if hadClient {
		if fn := m.getBeforeLeave(); fn != nil {
			fn(client, roomID)
		}
	}

	m.mu.Lock()
	rm, ok := m.rooms[roomID]
	if !ok {
		m.mu.Unlock()
		return
	}
	rm.Remove(clientID)
	if members, ok := m.membership[clientID]; ok {
		delete(members, roomID)
		if len(members) == 0 {
			delete(m.membership, clientID)
		}
	}
	empty := rm.Size() == 0
	if empty {
		delete(m.rooms, roomID)
	}
	m.mu.Unlock()

	if hadClient {
		if fn := m.getAfterLeave(); fn != nil {
			fn(client, roomID)
		}
	}

	m.broadcastMemberEvent(roomID, clientID, MemberLeft)

	if empty {
		rm.Close(context.Background())
		if fn := m.getRoomClosed(); fn != nil {
			fn(roomID)
		}
	}
}

// roomMember looks up the live knet.Client for clientID within roomID, if
// still present (used to pass a real client to OnBeforeLeave when possible).
func (m *Manager) roomMember(roomID, clientID string) (knet.Client, bool) {
	m.mu.Lock()
	rm, ok := m.rooms[roomID]
	m.mu.Unlock()
	if !ok {
		return nil, false
	}
	for _, c := range rm.Clients() {
		if c.ID() == clientID {
			return c, true
		}
	}
	return nil, false
}

// --- Helpers -----------------------------------------------------------------

func (m *Manager) broadcastMemberEvent(roomID, clientID, eventType string) {
	m.mu.Lock()
	rm, ok := m.rooms[roomID]
	m.mu.Unlock()
	if !ok {
		return
	}
	payload, err := json.Marshal(RoomMemberEvent{RoomID: roomID, ClientID: clientID, Type: eventType})
	if err != nil {
		return
	}
	rm.BroadcastExcept(context.Background(), clientID, CmdRoomMemberEvent, payload) //nolint:errcheck
}

func (m *Manager) send(client knet.Client, commandID uint32, v interface{}) {
	payload, err := json.Marshal(v)
	if err != nil {
		return
	}
	client.Send(context.Background(), commandID, payload) //nolint:errcheck
}

func (m *Manager) sendError(client knet.Client, roomID, code, message string) {
	m.send(client, CmdRoomError, RoomError{RoomID: roomID, Code: code, Message: message})
}

func (m *Manager) getBeforeJoin() func(knet.Client, string) error {
	m.hookMu.RLock()
	defer m.hookMu.RUnlock()
	return m.onBeforeJoin
}

func (m *Manager) getAfterJoin() func(knet.Client, string) {
	m.hookMu.RLock()
	defer m.hookMu.RUnlock()
	return m.onAfterJoin
}

func (m *Manager) getBeforeLeave() func(knet.Client, string) {
	m.hookMu.RLock()
	defer m.hookMu.RUnlock()
	return m.onBeforeLeave
}

func (m *Manager) getAfterLeave() func(knet.Client, string) {
	m.hookMu.RLock()
	defer m.hookMu.RUnlock()
	return m.onAfterLeave
}

func (m *Manager) getRoomCreated() func(string) {
	m.hookMu.RLock()
	defer m.hookMu.RUnlock()
	return m.onRoomCreated
}

func (m *Manager) getRoomClosed() func(string) {
	m.hookMu.RLock()
	defer m.hookMu.RUnlock()
	return m.onRoomClosed
}

func (m *Manager) getRoomMessage() func(knet.Client, string, string, string) error {
	m.hookMu.RLock()
	defer m.hookMu.RUnlock()
	return m.onRoomMessage
}

func setToSlice(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	return out
}

func memberIDs(rm room.Room) []string {
	if rm == nil {
		return nil
	}
	clients := rm.Clients()
	out := make([]string, 0, len(clients))
	for _, c := range clients {
		out = append(out, c.ID())
	}
	return out
}
