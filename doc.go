// Package knet provides a high-performance WebSocket library for game servers and real-time applications.
//
// This library implements a command pattern protocol with efficient binary encoding and optional
// JSON-RPC 2.0 support. It's designed for building scalable game servers, real-time communication
// systems, and any application requiring low-latency WebSocket messaging.
//
// # Architecture
//
// The library uses a command pattern where each message consists of a 4-byte command ID followed
// by a binary payload. Handlers are registered for specific command IDs, allowing clean separation
// of different message types (e.g., player movement, chat, inventory operations in a game server).
//
// Optional JSON-RPC 2.0 support is provided for standard RPC-style communication patterns.
//
// # Quick Start
//
//	import (
//	    "github.com/luciancaetano/knet/ws"
//	)
//
//	// Create server with rate limiting
//	rateLimitConfig := ws.DefaultRateLimitConfig() // 100 msgs/s, burst 200
//	server := ws.New(":8080", rateLimitConfig, ws.AllOrigins())
//
//	// Register command handlers (command pattern)
//	server.RegisterHandler(ctx, 0x01, func(client knet.Client, payload []byte) {
//	    // Process the message and optionally send a response
//	    response := []byte("pong")
//	    client.Send(ctx, 0x01, response)
//	})
//
//	// Optional: Register JSON-RPC handlers
//	server.RegisterJSONRPCHandler(ctx, "getStatus", func(params map[string]interface{}) (interface{}, error) {
//	    return map[string]string{"status": "ok"}, nil
//	})
//
//	server.Start(ctx)
//
// # Protocol Format
//
// The library uses a command pattern binary protocol:
//
//	[4 bytes: CommandID (uint32, big-endian)][N bytes: Payload]
//
// Each command ID maps to a registered handler function. This pattern is ideal for game servers
// where different message types (movement, combat, chat, etc.) need different processing logic.
//
// Maximum payload: 10MB. Zero-copy decode for performance.
//
// # JSON-RPC 2.0 Support
//
// In addition to binary commands, the library supports JSON-RPC 2.0 for standard RPC workflows.
// JSON-RPC messages use reserved command IDs (0xFFFFFFFF for requests, 0xFFFFFFFE for errors).
//
// # Timing and Round-Trip Time
//
// The timing package's TimeManager drives fixed-rate tick broadcasts and tracks
// tick-relative timing: tick lifecycle hooks, server uptime, tick↔duration
// conversions, and optional per-client round-trip time via ping/pong (see
// TimeManager.EnablePing). Ping/pong uses reserved command ID 0xFFFFFFFD.
//
// Room grouping, interest-managed broadcasts, and dirty-tracked state sync
// are opt-in features in the room, observer, and syncvar packages,
// respectively.
//
// # Rate Limiting
//
// Each client has independent rate limiting using token bucket algorithm:
//
//	// Default: 100 messages/second, burst 200
//	rateLimitConfig := ws.DefaultRateLimitConfig()
//	server := ws.New(":8080", rateLimitConfig, ws.AllOrigins())
//
//	// Custom: 50 messages/second, burst 100
//	rateLimitConfig := &ws.RateLimitConfig{
//	    MessagesPerSecond: 50,
//	    Burst:             100,
//	    Enabled:           true,
//	}
//	server := ws.New(":8080", rateLimitConfig, ws.AllOrigins())
//
//	// Disabled
//	rateLimitConfig := ws.NoRateLimit()
//	server := ws.New(":8080", rateLimitConfig, ws.AllOrigins())
//
// When rate limit is exceeded, client receives close code 1008 (Policy Violation).
//
// # Security Features
//
//   - Rate limiting per client (prevents DoS)
//   - Maximum payload: 10MB (prevents OOM)
//   - Read timeout: 60s (prevents hanging)
//   - Write timeout: 10s (prevents slow clients)
//   - Automatic keepalive with pong handler
//   - Origin validation via CheckOriginFn
//
// # Performance
//
//   - Zero-copy decode (payload references original buffer)
//   - Async handlers (don't block read loop)
//   - 256-message buffer per client
//   - Ping every 54 seconds
//
// # Important
//
//   - DO NOT modify decoded payload (it references the original buffer)
//   - Handlers execute in goroutines (no execution order guarantee)
//   - Configure CheckOriginFn in production (never use ws.AllOrigins() in production)
package knet
