package knet

import "context"

// HandlerFunc is the signature of a binary-command message handler.
// Handlers are registered via [Server.RegisterHandler] and executed
// asynchronously in the server's worker pool.
type HandlerFunc func(client Client, payload []byte)

// JSONRPCHandler is the signature of a JSON-RPC 2.0 method handler.
// Handlers are registered via [Server.RegisterJSONRPCHandler].
// For a type-safe alternative see [HandleJSONRPC].
type JSONRPCHandler func(params map[string]interface{}) (interface{}, error)

// Server defines the interface for a WebSocket server that uses binary
// protocol encoding.
//
// All messages exchanged between the server and clients are encoded using the
// internal protocol format: a CommandID (uint32) followed by a binary Payload.
//
// Example usage:
//
//	import "github.com/luciancaetano/knet/ws"
//
//	server := ws.New(ws.NewConfig(
//	    ":8080",
//	    ws.DefaultRateLimitConfig(),
//	    ws.AllOrigins(),
//	    nil, // OnConnect callback (optional)
//	    nil, // OnDisconnect callback (optional)
//	))
//
//	// Register a handler for command 0x01
//	server.RegisterHandler(ctx, 0x01, func(client knet.Client, payload []byte) {
//	    client.Send(ctx, 0x01, []byte("pong"))
//	})
//
//	server.Start(ctx)
type Server interface {
	// Start starts the WebSocket server and begins listening for connections.
	// The server will continue running until Stop is called or the context is cancelled.
	//
	// Returns an error if the server is already running or if there's a problem
	// binding to the network address.
	Start(ctx context.Context) error

	// Stop gracefully stops the WebSocket server and closes all client connections.
	// Active connections are given time to close properly.
	//
	// Returns an error if there's a problem during shutdown.
	Stop(ctx context.Context) error

	// RegisterHandler registers a handler function for a specific command ID.
	//
	// The handler is executed asynchronously (fire-and-forget pattern).
	// It receives the client instance and payload, allowing you to send responses
	// or broadcast messages as needed. Unlike JSON-RPC, there's no automatic
	// request-response pairing.
	//
	// When a message with the given commandID is received from a client, the handler
	// function is called with the client and payload in a separate goroutine.
	//
	// Parameters:
	//   - ctx: Context for cancellation
	//   - commandID: The uint32 command identifier to handle
	//   - handler: Function that processes the payload with access to the client
	//
	// Example:
	//
	//	server.RegisterHandler(ctx, 0x0100, func(client Client, payload []byte) {
	//	    // Process message and optionally send response
	//	    response := processMessage(payload)
	//	    client.Send(ctx, 0x0100, response)
	//	})
	RegisterHandler(ctx context.Context, commandID uint32, handler HandlerFunc) error

	// RegisterJSONRPCHandler registers a JSON-RPC 2.0 method handler.
	//
	// For a type-safe version that avoids map[string]interface{} assertions,
	// use the [HandleJSONRPC] generic helper instead.
	//
	// Example:
	//
	//	server.RegisterJSONRPCHandler(ctx, "add", func(params map[string]interface{}) (interface{}, error) {
	//	    a := params["a"].(float64)
	//	    b := params["b"].(float64)
	//	    return a + b, nil
	//	})
	RegisterJSONRPCHandler(ctx context.Context, method string, handler JSONRPCHandler) error

	// BroadcastCommand sends a command to all connected clients.
	//
	// This method is useful for broadcasting messages to all connected clients,
	// such as chat messages, notifications, or system-wide updates.
	//
	// Parameters:
	//   - ctx: Context for cancellation
	//   - commandID: The uint32 command identifier
	//   - payload: The binary payload to send
	//
	// Example:
	//
	//	// Broadcast a notification to all clients
	//	data, _ := json.Marshal(notification)
	//	server.BroadcastCommand(ctx, 0x0100, data)
	BroadcastCommand(ctx context.Context, commandID uint32, payload []byte) error
}

// Client represents a connected WebSocket client.
//
// Each client has a unique identifier and maintains its own connection state.
// The client's context is automatically cancelled when the connection closes.
//
// Example usage:
//
//	// Send a message to a client
//	encodedData, _ := protocol.Encode(0x01, []byte("hello"))
//	client.Send(ctx, encodedData)
//
//	// Check if client is still connected
//	if client.IsAlive() {
//	    // ...
//	}
type Client interface {
	// ID returns a unique identifier for the connected client.
	//
	// The ID is automatically generated when the client connects and remains
	// constant for the lifetime of the connection.
	ID() string

	// RemoteAddr returns the client's remote network address.
	//
	// This is typically in the format "IP:port", for example "192.168.1.100:54321".
	RemoteAddr() string

	// Context returns the client's lifecycle context.
	//
	// This context is automatically cancelled when the connection closes,
	// allowing goroutines and operations associated with the client to be
	// properly cleaned up.
	//
	// Example:
	//
	//	go func() {
	//	    <-client.Context().Done()
	//	    log.Printf("Client %s disconnected", client.ID())
	//	}()
	Context() context.Context

	// Send sends binary data to the client over the WebSocket connection.
	//
	// The command and payload are automatically encoded using the protocol format
	// before being sent. The send operation is non-blocking and queued for delivery.
	//
	// Returns an error if the connection is closed or the context is cancelled.
	//
	// Example:
	//
	//	if err := client.Send(ctx, 0x01, []byte("message")); err != nil {
	//	    log.Printf("Failed to send: %v", err)
	//	}
	Send(ctx context.Context, command uint32, payload []byte) error

	// Close closes the client connection gracefully.
	//
	// This is equivalent to calling CloseWithCode with websocket.CloseNormalClosure.
	Close(ctx context.Context) error

	// CloseWithCode closes the connection with a specific WebSocket close code and optional reason.
	//
	// Common close codes:
	//   - 1000 (websocket.CloseNormalClosure): Normal closure
	//   - 1001 (websocket.CloseGoingAway): Endpoint going away
	//   - 1002 (websocket.CloseProtocolError): Protocol error
	//   - 1003 (websocket.CloseUnsupportedData): Unsupported data
	//
	// Example:
	//
	//	client.CloseWithCode(ctx, 1000, "goodbye")
	CloseWithCode(ctx context.Context, code int, reason string) error

	// IsAlive returns true if the connection is still active.
	//
	// This can be used to check if a client is still connected before
	// attempting to send messages.
	//
	// Example:
	//
	//	if client.IsAlive() {
	//	    client.Send(ctx, data)
	//	}
	IsAlive() bool
}
