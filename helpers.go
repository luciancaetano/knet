package knet

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
)

// HandleJSONRPC registers a type-safe JSON-RPC 2.0 handler on srv.
//
// Unlike [Server.RegisterJSONRPCHandler], which exposes raw
// map[string]interface{} params and requires manual type assertions,
// HandleJSONRPC unmarshals the params into Req and marshals the return value
// as Resp automatically. A JSON decode failure is returned to the caller as a
// JSON-RPC invalid-request error.
//
// Example:
//
//	type MoveRequest  struct { X, Y float64 `json:"x,y"` }
//	type MoveResponse struct { OK   bool    `json:"ok"`  }
//
//	knet.HandleJSONRPC(ctx, server, "player.move",
//	    func(req MoveRequest) (MoveResponse, error) {
//	        return MoveResponse{OK: movePlayer(req.X, req.Y)}, nil
//	    },
//	)
func HandleJSONRPC[Req, Resp any](
	ctx context.Context,
	srv Server,
	method string,
	fn func(req Req) (Resp, error),
) error {
	return srv.RegisterJSONRPCHandler(ctx, method, func(params map[string]interface{}) (interface{}, error) {
		// Re-encode the params map so json.Unmarshal can apply struct tags,
		// type coercions, and nested-object handling into the concrete Req type.
		raw, err := json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("invalid request: failed to marshal params: %w", err)
		}

		var req Req
		if err := json.Unmarshal(raw, &req); err != nil {
			return nil, fmt.Errorf("invalid request: %w", err)
		}

		return fn(req)
	})
}

// ConnectHooks fans a single onConnect/onDisconnect slot (ws.Config accepts
// only one of each) out to multiple independent listeners — e.g. several
// Rooms that each need to track connect/disconnect without knowing about
// each other.
//
// Example:
//
//	hooks := &knet.ConnectHooks{}
//	hooks.OnConnect(lobby.OnConnect)
//	hooks.OnConnect(matchmaking.OnConnect)
//	hooks.OnDisconnect(lobby.OnDisconnect)
//
//	cfg := ws.NewConfig(":8080", ws.DefaultRateLimitConfig(), ws.AllOrigins(),
//	    hooks.DispatchConnect, hooks.DispatchDisconnect)
type ConnectHooks struct {
	mu     sync.Mutex
	onConn []func(Client) bool
	onDisc []func(Client, bool)
}

// OnConnect registers fn to run on every DispatchConnect call.
func (h *ConnectHooks) OnConnect(fn func(Client) bool) {
	h.mu.Lock()
	h.onConn = append(h.onConn, fn)
	h.mu.Unlock()
}

// OnDisconnect registers fn to run on every DispatchDisconnect call.
func (h *ConnectHooks) OnDisconnect(fn func(Client, bool)) {
	h.mu.Lock()
	h.onDisc = append(h.onDisc, fn)
	h.mu.Unlock()
}

// DispatchConnect runs every registered OnConnect listener in registration
// order. The first listener to return false stops the chain and rejects the
// client — later listeners do not run.
func (h *ConnectHooks) DispatchConnect(c Client) bool {
	h.mu.Lock()
	hooks := append([]func(Client) bool(nil), h.onConn...)
	h.mu.Unlock()

	for _, fn := range hooks {
		if !fn(c) {
			return false
		}
	}
	return true
}

// DispatchDisconnect runs every registered OnDisconnect listener in
// registration order.
func (h *ConnectHooks) DispatchDisconnect(c Client, voluntary bool) {
	h.mu.Lock()
	hooks := append(([]func(Client, bool))(nil), h.onDisc...)
	h.mu.Unlock()

	for _, fn := range hooks {
		fn(c, voluntary)
	}
}
