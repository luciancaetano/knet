package knet

import (
	"context"
	"encoding/json"
	"fmt"
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
