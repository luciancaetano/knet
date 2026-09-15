package bench

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/luciancaetano/knet"
	"github.com/luciancaetano/knet/clock"
	"github.com/luciancaetano/knet/internal/room"
	"github.com/luciancaetano/knet/timing"
	"github.com/luciancaetano/knet/ws"
)

// EchoCommandID is the single command every benchmark scenario registers:
// the server echoes the payload straight back to the sender.
const EchoCommandID uint32 = 1

// serverHandle bundles everything a scenario needs to drive load against a
// running knet server and tear it down afterward.
type serverHandle struct {
	Server  knet.Server
	Room    room.Room // nil when the scenario runs without RoomManager
	Timing  *timing.TimeManager
	Metrics *collectorMetrics
	Addr    string
	stop    func()
}

// startServer boots a knet ws server on an ephemeral local port, optionally
// wiring a Room and a Ticker (TimeManager) on top of it, matching the
// scenario matrix (baseline / room / ticker / room+ticker).
func startServer(withRoom, withTicker bool, tickInterval time.Duration) (*serverHandle, error) {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	addr := lis.Addr().String()
	lis.Close() //nolint:errcheck // just reserving a free port

	metrics := newCollectorMetrics()

	var rm room.Room
	if withRoom {
		rm = room.New("bench", tickInterval)
	}

	cfg := ws.NewConfig(addr, ws.NoRateLimit(), ws.AllOrigins(), func(c knet.Client) bool {
		if rm != nil {
			rm.Add(c)
		}
		return true
	}, func(c knet.Client, _ bool) {
		if rm != nil {
			rm.Remove(c.ID())
		}
	})
	cfg.Metrics = metrics

	server := ws.New(cfg)

	ctx, cancel := context.WithCancel(context.Background())

	if err := server.RegisterHandler(ctx, EchoCommandID, func(c knet.Client, payload []byte) {
		_ = c.Send(ctx, EchoCommandID, payload)
	}); err != nil {
		cancel()
		return nil, err
	}

	var tm *timing.TimeManager
	if withTicker {
		tm = timing.New(server, tickInterval)
		if err := tm.EnablePing(ctx, server, 1); err != nil {
			cancel()
			return nil, err
		}
		if rm != nil {
			tm.RegisterRoom(rm, 2, func(clock.Tick) []byte { return []byte("tick") })
		} else {
			tm.Register(2, func(clock.Tick) []byte { return []byte("tick") })
		}
		tm.Start(ctx)
	}

	go func() { _ = server.Start(ctx) }()
	// Give the listener a moment to come up before dialers race it.
	time.Sleep(50 * time.Millisecond)

	stop := func() {
		if tm != nil {
			tm.Stop()
		}
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = server.Stop(stopCtx)
		stopCancel()
		cancel()
	}

	return &serverHandle{Server: server, Room: rm, Timing: tm, Metrics: metrics, Addr: addr, stop: stop}, nil
}

func (h *serverHandle) Stop() { h.stop() }

// benchClient is a raw gorilla/websocket connection driven directly against
// the wire protocol (bypassing the knet client SDK) so the benchmark measures
// the protocol itself, matching tests/stress's approach.
type benchClient struct {
	conn      *websocket.Conn
	writeMu   sync.Mutex // gorilla/websocket forbids concurrent writes on one conn
	lastEcho  chan time.Time
	closeOnce func()
}

// connectClients dials n raw WebSocket connections against addr and starts a
// read loop on each that: (a) echoes back any PingCommandID frame unchanged
// (the client-side contract EnablePing/RTT rely on), and (b) timestamps
// EchoCommandID replies for round-trip latency sampling.
func connectClients(addr string, n int) ([]*benchClient, error) {
	clients := make([]*benchClient, 0, n)
	for i := 0; i < n; i++ {
		conn, _, err := websocket.DefaultDialer.Dial(fmt.Sprintf("ws://%s/ws", addr), nil)
		if err != nil {
			for _, c := range clients {
				c.conn.Close() //nolint:errcheck
			}
			return nil, fmt.Errorf("dial client %d/%d: %w", i, n, err)
		}
		bc := &benchClient{lastEcho: make(chan time.Time, 1)}
		bc.closeOnce = func() { conn.Close() } //nolint:errcheck
		bc.conn = conn

		go bc.readLoop()
		clients = append(clients, bc)
	}
	return clients, nil
}

const pingCommandID uint32 = 0xFFFFFFFD

func (c *benchClient) readLoop() {
	for {
		_, data, err := c.conn.ReadMessage()
		if err != nil {
			return
		}
		cmd, payload, ok := decodeFrame(data)
		if !ok {
			continue
		}
		switch cmd {
		case pingCommandID:
			// Echo the tick payload back unchanged: the entire client-side
			// contract EnablePing/RTT rely on (see timing/ping.go).
			c.writeMu.Lock()
			_ = c.conn.WriteMessage(websocket.BinaryMessage, encodeFrame(pingCommandID, payload))
			c.writeMu.Unlock()
		case EchoCommandID:
			select {
			case c.lastEcho <- time.Now():
			default:
			}
		}
	}
}

func (c *benchClient) sendEcho(payload []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.conn.WriteMessage(websocket.BinaryMessage, encodeFrame(EchoCommandID, payload))
}

func (c *benchClient) close() { c.closeOnce() }

func closeClients(clients []*benchClient) {
	for _, c := range clients {
		c.close()
	}
}
