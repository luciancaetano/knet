package websocket

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"golang.org/x/time/rate"

	"github.com/luciancaetano/knet"
	"github.com/luciancaetano/knet/internal/protocol"
)

// Client implements the knet.Client interface.
type Client struct {
	id           string
	conn         *websocket.Conn
	remoteAddr   string
	ctx          context.Context
	cancel       context.CancelFunc
	sendCh       chan []byte
	closeCh      chan []byte // buffered 1; signals writePump to send a close frame
	mu           sync.RWMutex
	closed       bool
	serverClosed atomic.Bool // true when the server (not the remote peer) initiated close
	rateLimiter  *rate.Limiter
	pingInterval time.Duration
}

// NewClient creates a new WebSocket client.
//
// pingInterval controls how often keepalive pings are sent to the remote peer.
// Pass 0 to use the default (54 seconds).
func NewClient(conn *websocket.Conn, remoteAddr string, rateLimitConfig *RateLimitConfig, pingInterval time.Duration) *Client {
	ctx, cancel := context.WithCancel(context.Background())

	var limiter *rate.Limiter
	if rateLimitConfig != nil && rateLimitConfig.Enabled {
		limiter = rate.NewLimiter(rateLimitConfig.MessagesPerSecond, rateLimitConfig.Burst)
	}

	if pingInterval <= 0 {
		pingInterval = 54 * time.Second
	}

	client := &Client{
		id:           uuid.New().String(),
		conn:         conn,
		remoteAddr:   remoteAddr,
		ctx:          ctx,
		cancel:       cancel,
		sendCh:       make(chan []byte, 256),
		closeCh:      make(chan []byte, 1),
		rateLimiter:  limiter,
		pingInterval: pingInterval,
	}

	// writePump is the SOLE goroutine allowed to write to client.conn.
	// This satisfies gorilla/websocket's "one concurrent writer" invariant and
	// eliminates the CRIT-1 data race between CloseWithCode and writePump.
	go client.writePump()

	return client
}

// ID returns the client's unique identifier.
func (c *Client) ID() string { return c.id }

// RemoteAddr returns the client's remote network address.
func (c *Client) RemoteAddr() string { return c.remoteAddr }

// Context returns the client's lifecycle context, cancelled when the
// connection is closed.
func (c *Client) Context() context.Context { return c.ctx }

// Send encodes a command+payload and enqueues the message for delivery.
//
// The read-lock is released before blocking on the channel so that a
// concurrent Close (which requires a write-lock) is never starved by callers
// waiting on a full send buffer (MED-1 fix).
func (c *Client) Send(ctx context.Context, command uint32, payload []byte) error {
	data, err := protocol.Encode(command, payload)
	if err != nil {
		return fmt.Errorf("%s: %w", knet.ErrFailedToEncode, err)
	}

	// Snapshot the closed flag, then release before blocking on the channel.
	c.mu.RLock()
	closed := c.closed
	c.mu.RUnlock()

	if closed {
		return fmt.Errorf(knet.ErrConnectionClosed)
	}

	select {
	case c.sendCh <- data:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-c.ctx.Done():
		return fmt.Errorf(knet.ErrContextCancelled)
	}
}

// sendRaw enqueues a pre-encoded message without re-encoding.
//
// Unlike Send, this variant is non-blocking: when the send buffer is full the
// message is dropped rather than stalling the caller.  It is intended for
// broadcast paths where a single slow client must not delay all others
// (PERF-1 / PERF-6 fix).
func (c *Client) sendRaw(ctx context.Context, data []byte) error {
	c.mu.RLock()
	closed := c.closed
	c.mu.RUnlock()

	if closed {
		return fmt.Errorf(knet.ErrConnectionClosed)
	}

	select {
	case c.sendCh <- data:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-c.ctx.Done():
		return fmt.Errorf(knet.ErrContextCancelled)
	default:
		return fmt.Errorf("send buffer full, message dropped for client %s", c.id)
	}
}

// Close closes the connection with a normal closure code.
func (c *Client) Close(ctx context.Context) error {
	return c.CloseWithCode(ctx, websocket.CloseNormalClosure, "")
}

// CloseWithCode signals writePump to send a WebSocket close frame and cancels
// the client context.
//
// CRIT-1 fix: this method never writes to c.conn directly.  All writes are
// owned exclusively by writePump, eliminating the concurrent-write data race.
func (c *Client) CloseWithCode(ctx context.Context, code int, reason string) error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	c.mu.Unlock()

	// Deliver the close-frame payload to writePump.  Non-blocking: if writePump
	// has already exited the channel is simply never drained and the GC cleans up.
	msg := websocket.FormatCloseMessage(code, reason)
	select {
	case c.closeCh <- msg:
	default:
	}

	// Cancel the context so that any goroutine waiting on client.Context().Done()
	// wakes up, and writePump's ctx-done case fires if closeCh was not consumed.
	c.cancel()
	return nil
}

// IsAlive returns true if the connection has not been closed.
func (c *Client) IsAlive() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return !c.closed
}

// CheckRateLimit returns true if the client is within its configured message
// rate.
func (c *Client) CheckRateLimit(ctx context.Context) bool {
	if c.rateLimiter == nil {
		return true
	}
	return c.rateLimiter.Allow()
}

// writePump is the sole goroutine permitted to write to c.conn.
//
// Invariant: no other goroutine may call WriteMessage, WriteControl,
// SetWriteDeadline, or any other write-path method on c.conn.
func (c *Client) writePump() {
	ticker := time.NewTicker(c.pingInterval)
	defer func() {
		ticker.Stop()
		// Ensure the context is cancelled even when writePump exits due to a
		// write error (rather than because Close/CloseWithCode was called).
		c.cancel()
		c.conn.Close()
	}()

	for {
		// Give close-signal priority: check closeCh before entering the full
		// select so a pending close frame is sent before any queued messages.
		select {
		case msg := <-c.closeCh:
			c.conn.SetWriteDeadline(time.Now().Add(time.Second))
			c.conn.WriteControl(websocket.CloseMessage, msg, time.Now().Add(time.Second))
			return
		default:
		}

		select {
		case msg := <-c.closeCh:
			c.conn.SetWriteDeadline(time.Now().Add(time.Second))
			c.conn.WriteControl(websocket.CloseMessage, msg, time.Now().Add(time.Second))
			return

		case message, ok := <-c.sendCh:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				return
			}
			if err := c.conn.WriteMessage(websocket.BinaryMessage, message); err != nil {
				return
			}

		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}

		case <-c.ctx.Done():
			return
		}
	}
}

// SetPongHandler sets the pong-message handler on the underlying connection.
// Must be called before the read loop starts (not concurrent-safe with reads).
func (c *Client) SetPongHandler(handler func(appData string) error) {
	c.conn.SetPongHandler(handler)
}

// SetCloseHandler sets the close-message handler on the underlying connection.
func (c *Client) SetCloseHandler(handler func(code int, text string) error) {
	c.conn.SetCloseHandler(handler)
}
