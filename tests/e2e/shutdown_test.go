package e2e_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/luciancaetano/knet"
	"github.com/luciancaetano/knet/internal/protocol"
	"github.com/luciancaetano/knet/ws"
)

const cmdSlow uint32 = 0x0009

// TestShutdownDrainForcesStragglers starts a server with a slow in-flight
// handler, calls Stop with a short DrainTimeout, and verifies the client
// still connected at timeout is force-closed with GoingAway, and that Stop
// does not return before the drain window / in-flight handler settle.
func TestShutdownDrainForcesStragglers(t *testing.T) {
	t.Parallel()

	cfg := ws.NewConfig(":18083", ws.DefaultRateLimitConfig(), ws.AllOrigins(), nil, nil)
	cfg.DrainTimeout = 300 * time.Millisecond
	server := ws.New(cfg)
	ctx := context.Background()

	handlerStarted := make(chan struct{})
	server.RegisterHandler(ctx, cmdSlow, func(client knet.Client, payload []byte) {
		close(handlerStarted)
		time.Sleep(2 * time.Second) // outlives DrainTimeout on purpose
	})

	if err := server.Start(ctx); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	conn, _, err := newDialer().Dial("ws://localhost:18083/ws", nil)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close()

	encoded, _ := protocol.Encode(cmdSlow, []byte("go"))
	if err := conn.WriteMessage(websocket.BinaryMessage, encoded); err != nil {
		t.Fatalf("failed to send: %v", err)
	}

	select {
	case <-handlerStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("handler never started")
	}

	stopStart := time.Now()
	stopCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := server.Stop(stopCtx); err != nil {
		t.Fatalf("Stop returned error: %v", err)
	}
	stopElapsed := time.Since(stopStart)

	if stopElapsed < cfg.DrainTimeout {
		t.Errorf("Stop returned after %v, want at least DrainTimeout %v", stopElapsed, cfg.DrainTimeout)
	}

	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, _, err = conn.ReadMessage()
	closeErr, ok := err.(*websocket.CloseError)
	if !ok {
		t.Fatalf("expected a CloseError from the forced close, got: %v", err)
	}
	if closeErr.Code != websocket.CloseGoingAway {
		t.Errorf("close code = %d, want %d (CloseGoingAway)", closeErr.Code, websocket.CloseGoingAway)
	}
}

// TestShutdownDrainReturnsEarlyOnVoluntaryDisconnect verifies Stop doesn't
// wait the full DrainTimeout when the only connected client disconnects on
// its own before the timeout elapses.
func TestShutdownDrainReturnsEarlyOnVoluntaryDisconnect(t *testing.T) {
	t.Parallel()

	cfg := ws.NewConfig(":18084", ws.DefaultRateLimitConfig(), ws.AllOrigins(), nil, nil)
	cfg.DrainTimeout = 5 * time.Second
	server := ws.New(cfg)
	ctx := context.Background()

	if err := server.Start(ctx); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	conn, _, err := newDialer().Dial("ws://localhost:18084/ws", nil)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}

	// Client disconnects voluntarily right away.
	conn.Close()
	time.Sleep(100 * time.Millisecond)

	stopStart := time.Now()
	stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Stop(stopCtx); err != nil {
		t.Fatalf("Stop returned error: %v", err)
	}
	stopElapsed := time.Since(stopStart)

	if stopElapsed >= cfg.DrainTimeout {
		t.Errorf("Stop took %v, expected to return well before DrainTimeout %v since the client already left", stopElapsed, cfg.DrainTimeout)
	}
}

// TestShutdownRejectsNewConnections verifies handleWebSocket returns 503 for
// upgrade attempts once Stop has flipped the running flag. Stop is run in a
// goroutine with a DrainTimeout long enough to keep the HTTP listener open
// while we probe it, since the listener itself only closes at the very end
// of Stop.
func TestShutdownRejectsNewConnections(t *testing.T) {
	t.Parallel()

	cfg := ws.NewConfig(":18085", ws.DefaultRateLimitConfig(), ws.AllOrigins(), nil, nil)
	cfg.DrainTimeout = 2 * time.Second
	server := ws.New(cfg)
	ctx := context.Background()

	if err := server.Start(ctx); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	// Keep a client connected so clientWg stays non-zero and Stop actually
	// sits in the drain window instead of returning (and closing the
	// listener) almost immediately.
	conn, _, err := newDialer().Dial("ws://localhost:18085/ws", nil)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close()

	stopDone := make(chan struct{})
	go func() {
		defer close(stopDone)
		stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		server.Stop(stopCtx) //nolint:errcheck
	}()

	// Give Stop time to flip running=false but stay inside the drain window.
	time.Sleep(100 * time.Millisecond)

	resp, err := http.Get("http://localhost:18085/ws")
	if err != nil {
		t.Fatalf("expected the listener to still be open during drain, got: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusServiceUnavailable)
	}

	<-stopDone
}
