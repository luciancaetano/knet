# SyncVar & Timing

Both packages are opt-in, built for game loops that broadcast world state at a fixed tick rate (commonly 20 Hz / 50 ms).

## TimeManager (tick loop)

`timing.TimeManager` decouples your simulation frequency from the network update rate: game logic runs at any speed while `TimeManager` broadcasts snapshots at a predictable cadence.

```go
import (
	"github.com/luciancaetano/knet/clock"
	"github.com/luciancaetano/knet/timing"
)

tm := timing.New(server, 50*time.Millisecond) // 20 Hz

// Global broadcast: send world state to every connected client
tm.Register(WorldStateCmd, func(t clock.Tick) []byte {
	return world.Snapshot()
})

// Room-scoped: send positions only to players in the match
tm.RegisterRoom(matchRoom, PlayerPosCmd, func(t clock.Tick) []byte {
	if t.CurrentTick()%20 == 0 {
		return match.FullPositions() // full snapshot once a second
	}
	return match.DeltaPositions() // deltas in between
})

ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
defer stop()

tm.Start(ctx) // non-blocking, runs in a background goroutine
server.Start(ctx)
```

A `TickFn` returning `nil` skips the broadcast for that tick — use this for delta strategies where nothing changed. `t clock.Tick` exposes `t.ElapsedTime()`, `t.DeltaTime()`, `t.CurrentTime()`, and `t.CurrentTick()`.

### API

| Method | Description |
|--------|-------------|
| `timing.New(server, interval) *TimeManager` | interval typically 50–100ms (10–20 Hz) |
| `Register(commandID, fn TickFn) *TimeManager` | global broadcast handler every tick |
| `RegisterRoom(room, commandID, fn TickFn) *TimeManager` | broadcast scoped to one room |
| `RegisterObserverSet(set *observer.Set, subject, commandID, fn TickFn) *TimeManager` | broadcast filtered by interest — see [Rooms & Observer](rooms-observer.md) |
| `OnPreTick(fn TickHookFn)` / `OnPostTick(fn TickHookFn)` | side-effect hooks with no broadcast, e.g. syncing buffered input before tick handlers run |
| `Start(ctx)` / `Stop()` | begin/end the tick loop |
| `CurrentTick() uint64` | current tick counter |
| `Uptime() time.Duration` | time since `Start` |
| `TicksToTime(ticks) time.Duration` / `TimeToTicks(d) uint64` | convert between ticks and durations using `interval` |

Multiple handlers can be registered on one `TimeManager` — each gets its own independent call every tick.

### RTT (ping/pong)

```go
tm.EnablePing(ctx, server, 20) // ping every 20 ticks
rtt, ok := tm.RTT(client.ID())
```

Broadcasts the current tick on the reserved `timing.PingCommandID` (`0xFFFFFFFD`) every `intervalTicks`. Clients must echo the payload back unchanged on the same command ID — that's the entire client contract (`@lcaetano/knet-client` and the Unity client both do this automatically).

## Clock (setInterval/setTimeout for game loops)

`clock.Clock` is a fixed-rate scheduler for one-off or repeating callbacks scoped to a single tick loop — a JS `setInterval`/`setTimeout` equivalent, with pause/resume/reset/clear per registration. Each [Room](rooms-observer.md) owns one, created stopped, via `room.Clock()`:

```go
c := someRoom.Clock() // Room.Clock() — stopped until you call Start()
c.Start()

inst := c.SetInterval(200*time.Millisecond, func(t clock.Tick) {
	someRoom.Broadcast(ctx, WorldStateCmd, world.Snapshot())
})
// later: inst.Pause() / inst.Resume() / inst.Reset() / inst.Clear()
```

Durations are quantized to the clock's base tick interval (rounded to the nearest tick, minimum 1). A standalone clock not tied to a room can be created with `clock.New(interval)`.

### API

| Method | Description |
|--------|-------------|
| `clock.New(interval) Clock` | standalone clock, stopped until `Start()` |
| `room.Room.Clock() clock.Clock` | this room's own clock, created stopped |
| `SetInterval(d, fn func(t Tick)) ClockInstance` | repeating callback, quantized to `interval` |
| `SetTimeout(d, fn func(t Tick)) ClockInstance` | one-shot callback, auto-clears after firing |
| `ClearInterval(inst)` / `ClearTimeout(inst)` | equivalent to `inst.Clear()` |
| `ClockInstance.Pause()` / `Resume()` / `Reset()` / `Clear()` | per-registration control |
| `ClockInstance.Active()` / `Paused()` / `ElapsedTime()` | per-registration state |
| `Start()` / `Stop()` | begin/end the clock's tick loop |

## SyncVar (dirty-tracked state sync)

`syncvar.SyncVar[T]` holds server-authoritative state that's replicated only when it changes — avoids wasting bandwidth broadcasting unchanged values every tick.

```go
import "github.com/luciancaetano/knet/syncvar"

posX := syncvar.NewFloat32(0)

// Application code, whenever the value changes:
posX.Set(newX)

// TimeManager handler, every tick:
tm.RegisterRoom(room, PosCmd, func(t clock.Tick) []byte {
	payload, changed := posX.Flush()
	if !changed {
		return nil // TickFn returning nil skips the broadcast
	}
	return payload
})
```

### API

| Constructor | Wire type |
|---|---|
| `syncvar.NewInt32(initial int32)` | `TagInt32` |
| `syncvar.NewInt64(initial int64)` | `TagInt64` |
| `syncvar.NewFloat32(initial float32)` | `TagFloat32` |
| `syncvar.NewFloat64(initial float64)` | `TagFloat64` |
| `syncvar.NewBool(initial bool)` | `TagBool` |
| `syncvar.NewString(initial string)` | `TagString` |

| Method | Description |
|---|---|
| `Get() T` | current value |
| `Set(v T)` | update; marks dirty only if `v` differs from the current value (`==` comparison) |
| `Dirty() bool` | whether it changed since the last `Flush` |
| `Flush() (payload []byte, changed bool)` | encode + clear dirty flag; `(nil, false)` if nothing changed |
| `Tag() syncvar.Tag` | wire type |

SyncVar is safe for concurrent use.

### Wire format

Same contract on every client (`Knet.Unity SyncVarAttribute`, `@lcaetano/knet-client` syncvar helpers):

```
[0]     tag   — 1 byte, one of TagInt32(0x01) TagInt64(0x02) TagFloat32(0x03)
                TagFloat64(0x04) TagBool(0x05) TagString(0x06)
[1..]   value — UTF-8 text
                int32/int64/bool → decimal digits ("42", "true")
                float32/float64  → shortest round-trip decimal ("3.14")
                string           → raw bytes, no escaping
```

`syncvar.Decode(data []byte, typ Tag) (any, error)` parses a `Flush` payload back into a value — returns `syncvar.ErrTypeMismatch` if the payload's leading tag doesn't match `typ`, useful for validating incoming messages against a declared SyncVar type.
