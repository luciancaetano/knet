---
title: RoomManager — Optional Room Lifecycle, Join/Leave, and Reconnect Handling
version: 1.0
date_created: 2026-09-14
owner: knet maintainers
tags: [architecture, design, server, client, room]
---

# Introduction

knet currently provides only a low-level `room.Room` primitive: a concurrency-safe set of connected clients with `Add`/`Remove`/`Broadcast`. There is no concept of a named/discoverable room, no reserved "join"/"leave" protocol commands, and no automatic wiring between connect/disconnect/resume events and room membership. Both first-party clients (TypeScript in `clients/js`, Unity in `clients/unity`) have no room concept at all: applications that want rooms must hand-roll join/leave commands, track membership client-side, and manually call `room.Add`/`room.Remove` from their own `OnConnect`/`OnClientDisconnect` callbacks.

This specification defines **RoomManager**: an optional, opt-in layer — for the Go server and both clients — that provides automatic room lifecycle (create-on-first-join, close-on-empty), reserved `join`/`leave` commands, and room-scoped `disconnect`/`reconnect`/`leave` events, built entirely on knet's existing public extension points (`RegisterHandler`, `OnConnectFn`, `OnClientDisconnectFn`, `OnResumeFn`, `SessionStore`). RoomManager must not require any change to knet core, `room.Room`, or the wire protocol's reserved command IDs.

## 1. Purpose & Scope

**Purpose**: Give applications a ready-made, drop-in room system (server + client) so they don't need to reimplement join/leave/reconnect handling on top of the raw `room.Room` primitive every time.

**Scope**:
- Server-side: a `roommanager` Go package, attachable to a `knet.Server`/`ServerConfig` optionally, providing multi-room registry, automatic membership lifecycle, reserved join/leave commands, and room-scoped lifecycle events (client joined, left, disconnected, reconnected).
- Client-side: a `RoomManager` class for the TypeScript client (`clients/js`) and a `RoomManager` `MonoBehaviour`/component for the Unity client (`clients/unity`), each wrapping the existing `KNetClient`, exposing `joinRoom`/`leaveRoom`/`reconnect`-aware APIs and room membership events.

**Out of scope**: room-internal game/application state sync, matchmaking/room discovery UI, authorization/permissions for who may join a room (left to the application via `OnBeforeJoin` hook, see §3), persistence of room state across server restarts.

**Audience**: knet maintainers and application developers integrating knet into a multiplayer server/client. Assumes familiarity with the existing knet server (`/home/lucian/knet/kephasnet.go`, `/home/lucian/knet/internal/websocket/websocket_server.go`) and `room.Room` (`/home/lucian/knet/room/room.go`).

**Assumptions**:
- Readers have read `/home/lucian/knet/README.md` §"Rooms" and the `SessionStore`/`OnResume` documentation in `/home/lucian/knet/session.go`.
- This spec assumes a single knet server process per `RoomManager` instance (no built-in cross-process room sharding); multi-instance deployments must externalize room membership themselves (out of scope).

## 2. Definitions

- **Room**: a named group of connected clients, identified by a unique `RoomID` (string), with a bounded/unbounded set of member `ClientID`s.
- **RoomManager (server)**: the Go component that owns the room registry, handles join/leave commands, and wires room membership to connect/disconnect/resume events.
- **RoomManager (client)**: the TS/Unity component that wraps `KNetClient`, sends join/leave commands, tracks the client's own current room membership, and re-issues joins after reconnect.
- **Voluntary disconnect**: the client closed the connection itself (matches knet's existing `voluntary bool` passed to `OnClientDisconnectFn`).
- **Involuntary disconnect**: the connection dropped without an explicit client-initiated close (network failure, crash) — eligible for session resume within `SessionGraceTTL`.
- **Reconnect / Resume**: client reconnects using a previously issued session ID (existing knet mechanism: `?session=<id>` query param + `SessionStore` + `OnResumeFn`).
- **Membership event**: one of `joined`, `left`, `disconnected`, `reconnected`, broadcast to a room's remaining members when another member's status changes.
- **Reserved command range**: the block of command IDs RoomManager reserves for its own join/leave/event protocol (see §4.1). Distinct from knet's own reserved IDs (`CmdJSONRPC=0xFFFFFFFF`, `CmdJSONRPCError=0xFFFFFFFE`).

## 3. Requirements, Constraints & Guidelines

### Server (`roommanager` package)

- **REQ-001**: RoomManager MUST be attachable to an existing `knet.Server`/`ServerConfig` without modifying knet core packages (`room`, `observer`, `internal/websocket`, top-level `knet`).
- **REQ-002**: RoomManager MUST be constructed explicitly by the application (e.g. `rm := roommanager.New(server, roommanager.Config{...})`) — it is never auto-attached. An application not using RoomManager sees zero behavior change.
- **REQ-003**: RoomManager MUST register handlers for reserved `join` and `leave` commands via `server.RegisterHandler`, using command IDs from the reserved range (§4.1).
- **REQ-004**: RoomManager MUST maintain a registry of `*room.Room` instances keyed by `RoomID`, reusing the existing `room.Room` type internally (do not reimplement broadcast/membership storage).
- **REQ-005**: A room MUST be created automatically on the first successful join to a `RoomID` that does not yet exist, and closed (removed from the registry, `room.Room.Close()` called) automatically when its membership reaches zero — unless `Config.PersistEmptyRooms` is set for a given room via `OnBeforeCreateRoom` (GUD, not required for v1; see CON-004).
- **REQ-006**: A client MUST be able to be a member of multiple rooms simultaneously; RoomManager tracks membership per `(ClientID, RoomID)` pair.
- **REQ-007**: On successful join, RoomManager MUST broadcast a `member-joined` event to the room's other existing members and reply to the joining client with a `join-ack` containing the room's current member list.
- **REQ-008**: On explicit leave (client-initiated `leave` command) or voluntary disconnect, RoomManager MUST remove the client from every room it was a member of and broadcast `member-left` to each affected room's remaining members.
- **REQ-009**: On involuntary disconnect (per knet's existing `voluntary=false` signal), RoomManager MUST NOT immediately remove the client from its rooms. Instead it MUST broadcast `member-disconnected` to each room's remaining members and keep the membership pending for up to `SessionGraceTTL` (read from the server's configured value, or `Config.GraceTTL` if RoomManager defines its own — see CON-002).
- **REQ-010**: If the disconnected client reconnects within the grace period (resumes its session), RoomManager MUST rejoin it to its previous rooms automatically (no explicit `join` command required from the client) and broadcast `member-reconnected` to each room's remaining members.
- **REQ-011**: If the grace period expires without resume, RoomManager MUST finalize the removal: drop the client from its rooms and broadcast `member-left` (not a second `member-disconnected`) to remaining members.
- **REQ-012**: RoomManager MUST expose Go callback hooks for application logic: `OnBeforeJoin(client, roomID) error` (return non-nil to reject the join with a reason sent back to the client), `OnAfterJoin(client, roomID)`, `OnBeforeLeave(client, roomID)`, `OnAfterLeave(client, roomID)`, `OnRoomCreated(roomID)`, `OnRoomClosed(roomID)`.
- **REQ-013**: RoomManager MUST expose a query API: `Room(roomID) (*room.Room, bool)`, `RoomsOf(clientID) []string`, `Rooms() []string`.
- **CON-001**: RoomManager MUST NOT overwrite an application-supplied `OnConnectFn`/`OnClientDisconnectFn`/`OnResumeFn` on `ServerConfig`. Since knet's `ServerConfig` holds a single function reference per hook (see `/home/lucian/knet/internal/websocket/websocket_server.go`), the application MUST wire RoomManager's hook methods into its own callback chain explicitly (RoomManager exposes `HandleConnect`, `HandleDisconnect(client, voluntary)`, `HandleResume(client, previousRooms) bool` methods for this purpose — not automatic interception). Document this wiring requirement prominently; it is the one unavoidable manual integration step.
- **CON-002**: RoomManager's join/leave/event protocol MUST use command IDs outside knet's existing reserved IDs (`0xFFFFFFFF`, `0xFFFFFFFE`) and MUST NOT collide with application-chosen command IDs by convention (reserved block documented in §4.1); RoomManager does not require changes to `internal/protocol/protocol.go`.
- **CON-003**: RoomManager relies on the existing `knet.SessionStore` for resume support. If the application did not configure a custom `SessionStore`/`SessionGraceTTL`, RoomManager MUST work correctly against the default in-memory store and its default 30s grace TTL.
- **CON-004**: v1 always closes empty rooms (no persistent/empty-room mode). `PersistEmptyRooms` is noted as a future extension point, not a v1 requirement.
- **GUD-001**: Room membership limits (`MaxMembersPerRoom`) SHOULD be supported as an optional `Config` field, enforced in the join handler by rejecting with an error payload; not a hard requirement for v1 but the design SHOULD leave room for it (an integer field checked before `room.Add`).
- **PAT-001**: Follow knet's existing pattern of small interfaces + explicit dependency injection (see `knet.Server`, `knet.SessionStore`) rather than global/singleton state — `roommanager.Manager` is an ordinary struct returned by `New(...)`, not a package-level singleton.

### Client (TypeScript `clients/js` and Unity `clients/unity`)

- **REQ-014**: Each client MUST provide a `RoomManager` (TS: class; Unity: `MonoBehaviour` or plain C# class attached alongside `KNetClient`) that wraps an existing `KNetClient` instance — constructed as `new RoomManager(client)` (TS) / `RoomManager.Attach(KNetClient client)` (Unity), not a subclass of `KNetClient`.
- **REQ-015**: RoomManager (client) MUST expose `joinRoom(roomId: string): Promise<RoomJoinResult>` (TS) / `JoinRoomAsync(string roomId)` (Unity) that sends the reserved join command and resolves/completes when `join-ack` or a rejection is received (with a timeout, mirroring existing client timeout patterns if any exist, else a configurable default e.g. 10s).
- **REQ-016**: RoomManager (client) MUST expose `leaveRoom(roomId: string): Promise<void>` / `LeaveRoomAsync(string roomId)` that sends the reserved leave command.
- **REQ-017**: RoomManager (client) MUST track the set of rooms the client currently believes it has joined (`currentRooms: string[]` / `IReadOnlyList<string> CurrentRooms`), updated on every successful join/leave/ack.
- **REQ-018**: RoomManager (client) MUST expose events for room membership changes broadcast by the server: `onMemberJoined(roomId, clientId)`, `onMemberLeft(roomId, clientId)`, `onMemberDisconnected(roomId, clientId)`, `onMemberReconnected(roomId, clientId)`.
- **REQ-019**: On the underlying `KNetClient`'s `disconnected` event, RoomManager (client) MUST NOT clear `currentRooms` immediately — it MUST wait to see whether reconnect (with session resume) succeeds, mirroring server-side grace-period semantics from the client's perspective.
- **REQ-020**: On successful reconnect, IF the underlying client used session resume, RoomManager (client) MUST trust the server's automatic rejoin (REQ-010) and simply re-sync `currentRooms` from the server's `join-ack`-equivalent resume payload, rather than re-sending explicit join commands for every previously joined room.
- **REQ-021**: IF reconnect happens without a resumable session (e.g. server-side grace period already expired, or the client never had `?session=` support enabled), RoomManager (client) MUST clear `currentRooms` and emit a `roomsLost` event so the application can decide whether to re-join manually.
- **CON-005**: Neither existing client (`clients/js/src/client.ts`, `clients/unity/KNetClient.cs`) currently appends a session ID to the reconnect URL/WebSocket URI. RoomManager (client) integration REQUIRES that `KNetClient` gain the ability to carry and resend a session ID across reconnects (see §8 dependency); this is a prerequisite change to `KNetClient`, not to RoomManager itself, and MUST be scoped and reviewed separately before RoomManager can rely on REQ-020.
- **CON-006**: RoomManager (client) MUST NOT introduce new WebSocket connections or bypass `KNetClient`'s existing connect/reconnect/backoff logic — it only sends/receives commands over the existing connection.
- **GUD-002**: TS and Unity RoomManager APIs SHOULD mirror each other in method names, event names, and semantics as closely as each language's idioms allow, to keep cross-platform application code easy to port.

## 4. Interfaces & Data Contracts

### 4.1 Reserved command range

RoomManager reserves a documented block of command IDs distinct from knet's built-in reserved IDs:

| Command | ID (uint32) | Direction | Payload |
|---|---|---|---|
| `CmdRoomJoin` | `0xFFFE0001` | client → server | `RoomJoinRequest` |
| `CmdRoomLeave` | `0xFFFE0002` | client → server | `RoomLeaveRequest` |
| `CmdRoomJoinAck` | `0xFFFE0003` | server → client | `RoomJoinAck` |
| `CmdRoomLeaveAck` | `0xFFFE0004` | server → client | `RoomLeaveAck` |
| `CmdRoomError` | `0xFFFE0005` | server → client | `RoomError` |
| `CmdRoomMemberEvent` | `0xFFFE0006` | server → client (broadcast) | `RoomMemberEvent` |
| `CmdRoomResumeSync` | `0xFFFE0007` | server → client | `RoomResumeSync` |

These constants MUST be defined once per language and kept in sync manually (Go: `roommanager.CmdRoomJoin` etc.; TS: `RoomCommands.Join` etc. in a new `clients/js/src/room-protocol.ts`; C#: `KNetRoomCommands.Join` etc. in a new `clients/unity/KNetRoomCommands.cs`), following the existing pattern of `KNetReservedCommands.cs` / `ReservedCommands` in `protocol.ts`.

### 4.2 Payload schemas (JSON over the existing frame payload, matching knet's JSON-RPC precedent)

```
RoomJoinRequest    { "roomId": string }
RoomLeaveRequest   { "roomId": string }
RoomJoinAck        { "roomId": string, "members": string[] }         // members = ClientIDs currently in room, including self
RoomLeaveAck       { "roomId": string }
RoomError          { "roomId": string, "code": string, "message": string }  // code e.g. "ROOM_FULL", "JOIN_REJECTED"
RoomMemberEvent    { "roomId": string, "clientId": string, "type": "joined"|"left"|"disconnected"|"reconnected" }
RoomResumeSync     { "rooms": [{ "roomId": string, "members": string[] }] }  // sent instead of per-room JoinAck after a session resume
```

### 4.3 Server Go API (illustrative signatures)

```go
package roommanager

type Config struct {
    GraceTTL          time.Duration // 0 = use server's SessionGraceTTL
    MaxMembersPerRoom int           // 0 = unlimited
}

type Manager struct { /* unexported */ }

func New(server knet.Server, cfg Config) *Manager

func (m *Manager) HandleConnect(client knet.Client) bool
func (m *Manager) HandleDisconnect(client knet.Client, voluntary bool)
func (m *Manager) HandleResume(client knet.Client, previousRooms []string) bool

func (m *Manager) Room(roomID string) (*room.Room, bool)
func (m *Manager) RoomsOf(clientID string) []string
func (m *Manager) Rooms() []string

func (m *Manager) OnBeforeJoin(fn func(client knet.Client, roomID string) error)
func (m *Manager) OnAfterJoin(fn func(client knet.Client, roomID string))
func (m *Manager) OnBeforeLeave(fn func(client knet.Client, roomID string))
func (m *Manager) OnAfterLeave(fn func(client knet.Client, roomID string))
func (m *Manager) OnRoomCreated(fn func(roomID string))
func (m *Manager) OnRoomClosed(fn func(roomID string))
```

### 4.4 TS client API (illustrative)

```ts
class RoomManager {
  constructor(client: KNetClient);
  joinRoom(roomId: string): Promise<{ roomId: string; members: string[] }>;
  leaveRoom(roomId: string): Promise<void>;
  readonly currentRooms: readonly string[];
  on(event: "memberJoined" | "memberLeft" | "memberDisconnected" | "memberReconnected",
     handler: (roomId: string, clientId: string) => void): void;
  on(event: "roomsLost", handler: () => void): void;
}
```

### 4.5 Unity client API (illustrative)

```csharp
public class KNetRoomManager {
    public static KNetRoomManager Attach(KNetClient client);
    public Task<RoomJoinResult> JoinRoomAsync(string roomId);
    public Task LeaveRoomAsync(string roomId);
    public IReadOnlyList<string> CurrentRooms { get; }
    public event Action<string, string> OnMemberJoined;
    public event Action<string, string> OnMemberLeft;
    public event Action<string, string> OnMemberDisconnected;
    public event Action<string, string> OnMemberReconnected;
    public event Action OnRoomsLost;
}
```

## 5. Acceptance Criteria

- **AC-001**: Given a server with RoomManager attached and wired per CON-001, When a client sends `CmdRoomJoin` for a new `roomId`, Then the server creates the room, adds the client, and replies with `CmdRoomJoinAck` containing `members: [clientId]`.
- **AC-002**: Given a room with clients A and B, When A sends `CmdRoomJoin` for that room, Then B receives a `CmdRoomMemberEvent` with `type: "joined"` and A's client ID, and A's ack lists both A and B.
- **AC-003**: Given a room with clients A and B, When A sends `CmdRoomLeave`, Then A is removed from the room, B receives `CmdRoomMemberEvent` with `type: "left"`, and if B was the last remaining member and also leaves, the room is removed from `Manager.Rooms()`.
- **AC-004**: Given a room with clients A and B, When A's connection drops involuntarily (not a clean close), Then B receives `CmdRoomMemberEvent` with `type: "disconnected"` and A remains in `Manager.RoomsOf(A)` until grace TTL expires or A resumes.
- **AC-005**: Given the state in AC-004, When A reconnects within the grace TTL using its prior session ID, Then A is automatically rejoined to the room without sending `CmdRoomJoin`, A receives `CmdRoomResumeSync` listing the room, and B receives `CmdRoomMemberEvent` with `type: "reconnected"`.
- **AC-006**: Given the state in AC-004, When A does not reconnect before grace TTL expires, Then A is removed from the room and B receives `CmdRoomMemberEvent` with `type: "left"` (not a second `"disconnected"`).
- **AC-007**: Given an `OnBeforeJoin` hook that returns an error for a given `(client, roomId)`, When that client sends `CmdRoomJoin` for that room, Then the server replies with `CmdRoomError` and does not add the client to the room or create it if it didn't exist.
- **AC-008**: Given a TS or Unity client with RoomManager attached, When the application calls `joinRoom(roomId)`, Then the returned promise/task resolves with the member list once `CmdRoomJoinAck` is received, or rejects/faults on `CmdRoomError` or timeout.
- **AC-009**: Given a TS or Unity client that disconnects and reconnects with a resumable session (post CON-005 prerequisite), When reconnect completes, Then `currentRooms` matches the server's `CmdRoomResumeSync` payload without the application re-issuing `joinRoom` calls.
- **AC-010**: Given an application that never constructs `roommanager.New(...)` / client `RoomManager`, Then knet's existing behavior (raw `room.Room`, manual wiring) is unaffected — no reserved command IDs are registered, no hooks are installed.

## 6. Test Automation Strategy

- **Test Levels**: Unit (Go: room registry, grace-period state machine, hook invocation order; TS/Unity: join/leave promise resolution, event dispatch, resume-sync reconciliation), Integration (server RoomManager wired to a real `knet.Server` via `internal/websocket`, exercised with a real or fake WebSocket client), End-to-End (TS client + Go server over a real socket; Unity client E2E if a Unity test harness exists in the repo, else covered by manual/editor testing).
- **Frameworks**: Go standard `testing` package + `testify`/assertions matching existing repo conventions (check `/home/lucian/knet/tests/e2e/reconnect_test.go` for the established E2E pattern to extend). TS: whatever test runner `clients/js` already uses (check `clients/js/package.json`). Unity: existing Unity test conventions in `clients/unity`, if any; otherwise document as a gap.
- **Test Data Management**: Ephemeral in-memory rooms/sessions per test; no shared fixtures, no external services required (default in-memory `SessionStore` suffices).
- **CI/CD Integration**: New Go tests added under `/home/lucian/knet/tests/e2e/` and a `roommanager` package's own `_test.go` files run under the existing Go test CI job; TS tests run under the existing `clients/js` CI job (if any exists — verify before assuming).
- **Coverage Requirements**: Every REQ-0xx and AC-0xx above should map to at least one automated test; grace-period timing tests should use fakeable/injectable clocks or short TTLs to avoid slow tests (mirror however `/home/lucian/knet/tests/e2e/reconnect_test.go` handles `SessionGraceTTL` timing today).
- **Performance Testing**: Not required for v1; note as future work if RoomManager is expected to handle very large rooms or very high join/leave churn (`MaxMembersPerRoom`, broadcast fan-out cost) — `room.Room.Broadcast` performance already exists and is inherited, not reinvented.

## 7. Rationale & Context

The research behind this spec found:

- knet has no `/spec/` directory prior to this document, no reserved join/leave opcode, and no `RoomManager` type in server or either client (`room.Room` is the only room-shaped primitive, and it is a bare, unmanaged container — see `/home/lucian/knet/room/room.go:35-171`).
- The server already has the two extension points this design depends on: `OnConnectFn`/`OnClientDisconnectFn` (`/home/lucian/knet/internal/websocket/websocket_server.go:36-43`), and a full session-resume mechanism (`SessionStore`, `OnResumeFn`, `SessionGraceTTL` — `/home/lucian/knet/session.go`, `/home/lucian/knet/internal/websocket/websocket_server.go:99-121`) that is fully scaffolded but currently unused by any built-in feature — RoomManager is the first consumer of `OnResumeFn`'s `previousRooms` parameter in the codebase.
- Because `ServerConfig` holds single function references (not multi-subscriber event lists) for `OnConnectFn`/`OnClientDisconnectFn`/`OnResumeFn`, RoomManager cannot silently "subscribe" to these events — the application must explicitly call RoomManager's `HandleConnect`/`HandleDisconnect`/`HandleResume` from its own callbacks. This is documented as CON-001 rather than hidden, to avoid surprising behavior and to keep RoomManager's opt-in nature explicit and auditable.
- Neither client currently sends a session ID on reconnect (`clients/js/src/client.ts` and `clients/unity/KNetClient.cs` both open reconnect sockets with the same static URL, no `?session=`), so client-side automatic rejoin-on-resume (REQ-020) has a hard prerequisite (CON-005) on a small `KNetClient` change to persist and resend the session ID. This spec calls that out explicitly rather than silently assuming it exists, since building RoomManager's reconnect story on a nonexistent capability would produce a spec that can't actually be implemented as described.
- Reserved command IDs for RoomManager were chosen in the `0xFFFE00xx` sub-range specifically to avoid the two ranges already in ad-hoc use across the two clients (JS reserves `...FFFD` for `Ping`; Unity reserves `...FFFD`/`...FFFC` for different purposes — `InvalidCommand`/`CommandError`). This inconsistency between the two clients' existing reserved ranges is a pre-existing issue noted here for awareness but is out of scope to fix in this spec.

## 8. Dependencies & External Integrations

### External Systems
- None required beyond the existing WebSocket transport already used by knet.

### Third-Party Services
- None.

### Infrastructure Dependencies
- **INF-001**: Default in-memory `knet.SessionStore` is sufficient for single-instance deployments; multi-instance deployments needing shared room/session state across server processes are out of scope for v1 (would require a distributed `SessionStore` implementation, which knet already supports as an interface but RoomManager does not add).

### Data Dependencies
- None external; all state (room registry, membership, pending-disconnect timers) is in-process.

### Technology Platform Dependencies
- **PLT-001**: Go server package depends only on existing `knet`, `room` packages already in this module — no new external Go dependency.
- **PLT-002**: TS client RoomManager depends only on the existing `clients/js` `KNetClient`/protocol modules — no new npm dependency expected.
- **PLT-003**: Unity client RoomManager depends only on the existing `clients/unity` `KNetClient`/protocol files — no new Unity package dependency expected.

### Compliance Dependencies
- None identified.

**Prerequisite change (blocking, not part of RoomManager itself)**: `KNetClient` (both TS and Unity) must gain the ability to store a session ID returned/implied by the server and resend it as `?session=<id>` on reconnect, to satisfy CON-005/REQ-020. This should be scoped as its own small spec/change before or alongside RoomManager client work.

## 9. Examples & Edge Cases

```go
// Server wiring example
lobby := roommanager.New(server, roommanager.Config{GraceTTL: 30 * time.Second})

cfg := websocket.ServerConfig{
    OnConnectFn: func(c knet.Client) bool {
        return lobby.HandleConnect(c) // app can add its own logic here too
    },
    OnClientDisconnectFn: func(c knet.Client, voluntary bool) {
        lobby.HandleDisconnect(c, voluntary)
    },
    OnResume: func(c knet.Client, previousRooms []string) bool {
        return lobby.HandleResume(c, previousRooms)
    },
}
```

Edge cases to handle explicitly:
- Client sends `CmdRoomJoin` for a room it has already joined → server MUST treat as idempotent success (re-ack with current member list), not an error.
- Client sends `CmdRoomLeave` for a room it is not a member of → server MUST reply `CmdRoomError` with code `NOT_A_MEMBER`, not crash or silently succeed.
- Client disconnects involuntarily while mid-join (join request sent, ack not yet received) → server MUST NOT leave the client half-joined; either complete the join before processing disconnect (ordering guaranteed by single-goroutine-per-connection dispatch, per existing knet handler model) or roll it back.
- Two different reconnect attempts arrive for the same expired session (race) → server MUST honor only the first valid resume and reject/ignore the second (existing `SessionStore` semantics apply; RoomManager does not need new locking beyond what `room.Room`'s existing `sync` already provides, but must not double-broadcast `member-reconnected`).
- Room reaches `MaxMembersPerRoom` (if configured) → join MUST be rejected with `CmdRoomError` code `ROOM_FULL`, room is not created if this is what would have created it.

## 10. Validation Criteria

- All acceptance criteria in §5 pass as automated tests.
- `roommanager` package compiles and is importable without any edit to `room`, `internal/websocket`, `session.go`, or `kephasnet.go`.
- An application with zero RoomManager usage shows no behavioral or performance change (AC-010).
- TS and Unity `RoomManager` public APIs reviewed for naming/semantic parity per GUD-002.
- CON-005 prerequisite (`KNetClient` session-id persistence across reconnect) is tracked and resolved before REQ-020/AC-009 are marked implemented.

## 11. Related Specifications / Further Reading

- `/home/lucian/knet/README.md` — existing "Rooms" usage pattern (manual `lobby.Add`/`lobby.Remove`).
- `/home/lucian/knet/session.go` — `SessionStore`, `OnResumeFn` (existing reconnect/resume scaffolding this spec builds on).
- `/home/lucian/knet/room/room.go` — `room.Room` primitive reused internally by RoomManager.
- `/home/lucian/knet/observer/observer.go` — sibling opt-in layer (`observer.Set`) showing the established pattern of building optional features on top of `room.Room` without modifying it.
- `/home/lucian/knet/tests/e2e/reconnect_test.go` — existing E2E test pattern for session resume, to extend for RoomManager reconnect tests.
