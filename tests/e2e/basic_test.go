package e2e_test

import (
	"context"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/luciancaetano/knet"
	"github.com/luciancaetano/knet/internal/protocol"
	"github.com/luciancaetano/knet/ws"
)

func TestBasicEcho(t *testing.T) {
	t.Parallel()

	server := ws.New(ws.NewConfig(":18080", ws.DefaultRateLimitConfig(), ws.AllOrigins(), nil, nil))
	ctx := context.Background()

	const cmdEcho uint32 = 0x0001
	// Handler receives client and payload, sends response asynchronously
	_ = server.RegisterHandler(ctx, cmdEcho, func(client knet.Client, payload []byte) {
		// Echo back to the client
		_ = client.Send(context.Background(), cmdEcho, payload)
	})

	if err := server.Start(ctx); err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}

	defer func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Stop(stopCtx)
	}()

	time.Sleep(200 * time.Millisecond)

	conn, _, err := newDialer().Dial("ws://localhost:18080/ws", nil)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer func() { _ = conn.Close() }()

	testPayload := []byte("Hello!")
	encoded, _ := protocol.Encode(cmdEcho, testPayload)

	if err := conn.WriteMessage(websocket.BinaryMessage, encoded); err != nil {
		t.Fatalf("Failed to send: %v", err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, response, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("Failed to read: %v", err)
	}

	_, _, respPayload, err := protocol.Decode(response)
	if err != nil {
		t.Fatalf("Failed to decode: %v", err)
	}

	if string(respPayload) != string(testPayload) {
		t.Errorf("got %q, want %q", respPayload, testPayload)
	}
}
