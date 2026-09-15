package timing

import (
	"context"
	"encoding/binary"
	"time"

	"github.com/luciancaetano/knet"
	"github.com/luciancaetano/knet/clock"
)

// PingCommandID is the reserved command ID used by [TimeManager]'s
// ping/pong round-trip-time measurement. It sits alongside the reserved
// JSON-RPC command IDs (see doc.go).
//
// Clients that want RTT tracking must echo back, unmodified, any payload
// received on PingCommandID (an 8-byte big-endian tick number) — that's the
// entire client-side contract.
const PingCommandID uint32 = 0xFFFFFFFD

// EnablePing turns on periodic ping broadcasts and RTT tracking.
//
// Every intervalTicks ticks, the TimeManager broadcasts the current tick
// number on [PingCommandID] to every connected client. Clients are expected
// to echo the payload back unchanged on the same command ID; EnablePing
// registers the handler that receives those echoes and computes RTT.
//
// intervalTicks must be > 0. Calling EnablePing more than once replaces the
// previous ping hook and re-registers the handler.
//
// RTT tracking is entirely opt-in — TimeManager does nothing on
// PingCommandID unless EnablePing is called.
func (t *TimeManager) EnablePing(ctx context.Context, server knet.Server, intervalTicks uint64) error {
	if intervalTicks == 0 {
		intervalTicks = 1
	}

	t.rttMu.Lock()
	if t.rtts == nil {
		t.rtts = make(map[string]uint64)
	}
	t.rttMu.Unlock()

	t.OnPostTick(func(tk clock.Tick) {
		tick := tk.CurrentTick()
		if tick%intervalTicks != 0 {
			return
		}
		payload := make([]byte, 8)
		binary.BigEndian.PutUint64(payload, tick)
		server.BroadcastCommand(ctx, PingCommandID, payload) //nolint:errcheck
	})

	return server.RegisterHandler(ctx, PingCommandID, func(client knet.Client, payload []byte) {
		if len(payload) != 8 {
			return
		}
		pingTick := binary.BigEndian.Uint64(payload)
		now := t.CurrentTick()
		if now < pingTick {
			return
		}
		rttTicks := now - pingTick

		t.rttMu.Lock()
		t.rtts[client.ID()] = rttTicks
		t.rttMu.Unlock()
	})
}

// RTT returns the last measured round-trip time for clientID, converted to a
// duration via [TimeManager.TicksToTime]. ok is false if no ping/pong has
// completed for that client yet (or [TimeManager.EnablePing] was never
// called).
func (t *TimeManager) RTT(clientID string) (rtt time.Duration, ok bool) {
	t.rttMu.RLock()
	rttTicks, found := t.rtts[clientID]
	t.rttMu.RUnlock()
	if !found {
		return 0, false
	}
	return t.TicksToTime(rttTicks), true
}
