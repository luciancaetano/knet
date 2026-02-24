package ws

import (
	"net/http"
	"strings"

	"github.com/luciancaetano/knet"
	"github.com/luciancaetano/knet/internal/websocket"
)

// Type aliases re-exported for callers that import only this package.
type RateLimitConfig = websocket.RateLimitConfig
type CheckOriginFn = websocket.CheckOriginFn

// OnConnectFn is called after a WebSocket handshake completes.
//
// BREAKING CHANGE: the function now returns bool.  Return true to accept the
// connection; return false to reject it with a policy-violation close code.
// This enables authentication checks to be enforced at the API level.
type OnConnectFn = websocket.OnConnectFn

type OnDisconnectFn = websocket.OnClientDisconnectFn
type ServerConfig = *websocket.ServerConfig

// New creates a new WebSocket server from the provided config.
func New(cfg ServerConfig) knet.WebsocketServer {
	return websocket.New(cfg)
}

// NewConfig is the recommended way to construct a ServerConfig.
//
// For advanced options (TLS, connection limits, worker-pool tuning) build the
// *websocket.ServerConfig struct directly and pass it to New.
func NewConfig(
	addr string,
	rateLimitConfig *RateLimitConfig,
	checkOrigin CheckOriginFn,
	onConnect OnConnectFn,
	onDisconnect OnDisconnectFn,
) ServerConfig {
	return &websocket.ServerConfig{
		Addr:               addr,
		RateLimitConfig:    rateLimitConfig,
		CheckOrigin:        checkOrigin,
		OnConnect:          onConnect,
		OnClientDisconnect: onDisconnect,
	}
}

// AllOrigins returns a CheckOriginFn that allows every origin.
//
// WARNING: only use this during local development.  In production, use
// AllowedOrigins to restrict access to known origins and prevent cross-site
// WebSocket hijacking (CSWSH).
func AllOrigins() CheckOriginFn {
	return func(r *http.Request) bool {
		return true
	}
}

// AllowedOrigins returns a CheckOriginFn that accepts connections only from
// the provided set of origins (case-insensitive comparison).
//
// Example:
//
//	ws.AllowedOrigins([]string{
//	    "https://game.example.com",
//	    "https://staging.example.com",
//	})
func AllowedOrigins(origins []string) CheckOriginFn {
	allowed := make(map[string]struct{}, len(origins))
	for _, o := range origins {
		allowed[strings.ToLower(strings.TrimRight(o, "/"))] = struct{}{}
	}
	return func(r *http.Request) bool {
		origin := strings.ToLower(strings.TrimRight(r.Header.Get("Origin"), "/"))
		if origin == "" {
			return false
		}
		_, ok := allowed[origin]
		return ok
	}
}

// DefaultRateLimitConfig returns the default rate-limit configuration
// (100 msg/s, burst 200).
func DefaultRateLimitConfig() *RateLimitConfig {
	return websocket.DefaultRateLimitConfig()
}

// NoRateLimit returns a configuration with rate limiting disabled.
func NoRateLimit() *RateLimitConfig {
	return websocket.NoRateLimit()
}
