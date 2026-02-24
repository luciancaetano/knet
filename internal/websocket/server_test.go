package websocket

import (
	"net/http"
	"testing"

	"golang.org/x/time/rate"
)

// TestDefaultRateLimitConfig tests the default rate limit configuration
func TestDefaultRateLimitConfig(t *testing.T) {
	t.Parallel()

	config := DefaultRateLimitConfig()

	if config == nil {
		t.Fatal("DefaultRateLimitConfig() returned nil")
	}

	if !config.Enabled {
		t.Error("Expected rate limiting to be enabled by default")
	}

	if config.MessagesPerSecond != 100 {
		t.Errorf("MessagesPerSecond = %v, want 100", config.MessagesPerSecond)
	}

	if config.Burst != 200 {
		t.Errorf("Burst = %v, want 200", config.Burst)
	}
}

// TestNoRateLimit tests the no rate limit configuration
func TestNoRateLimit(t *testing.T) {
	t.Parallel()

	config := NoRateLimit()

	if config == nil {
		t.Fatal("NoRateLimit() returned nil")
	}

	if config.Enabled {
		t.Error("Expected rate limiting to be disabled")
	}
}

// TestRateLimitConfigValues tests various rate limit configurations
func TestRateLimitConfigValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		config      *RateLimitConfig
		wantMPS     rate.Limit
		wantBurst   int
		wantEnabled bool
	}{
		{
			name:        "default config",
			config:      DefaultRateLimitConfig(),
			wantMPS:     100,
			wantBurst:   200,
			wantEnabled: true,
		},
		{
			name:        "no rate limit",
			config:      NoRateLimit(),
			wantMPS:     0,
			wantBurst:   0,
			wantEnabled: false,
		},
		{
			name: "custom config",
			config: &RateLimitConfig{
				MessagesPerSecond: 50,
				Burst:             100,
				Enabled:           true,
			},
			wantMPS:     50,
			wantBurst:   100,
			wantEnabled: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if tt.config.MessagesPerSecond != tt.wantMPS {
				t.Errorf("MessagesPerSecond = %v, want %v", tt.config.MessagesPerSecond, tt.wantMPS)
			}

			if tt.config.Burst != tt.wantBurst {
				t.Errorf("Burst = %v, want %v", tt.config.Burst, tt.wantBurst)
			}

			if tt.config.Enabled != tt.wantEnabled {
				t.Errorf("Enabled = %v, want %v", tt.config.Enabled, tt.wantEnabled)
			}
		})
	}
}

// TestNewServer tests server creation with various configurations
func TestNewServer(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		addr            string
		rateLimitConfig *RateLimitConfig
		checkOrigin     CheckOriginFn
	}{
		{
			name:            "with default rate limit",
			addr:            ":8080",
			rateLimitConfig: DefaultRateLimitConfig(),
			checkOrigin:     nil,
		},
		{
			name:            "with no rate limit",
			addr:            ":8081",
			rateLimitConfig: NoRateLimit(),
			checkOrigin:     nil,
		},
		{
			name:            "with nil rate limit config",
			addr:            ":8082",
			rateLimitConfig: nil, // Should use default
			checkOrigin:     nil,
		},
		{
			name: "with custom rate limit",
			addr: ":8083",
			rateLimitConfig: &RateLimitConfig{
				MessagesPerSecond: 10,
				Burst:             20,
				Enabled:           true,
			},
			checkOrigin: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server := New(&ServerConfig{
				Addr:               tt.addr,
				RateLimitConfig:    tt.rateLimitConfig,
				CheckOrigin:        tt.checkOrigin,
				OnConnect:          nil,
				OnClientDisconnect: nil,
			})

			if server == nil {
				t.Fatal("New() returned nil")
			}

			if server.addr != tt.addr {
				t.Errorf("server.addr = %v, want %v", server.addr, tt.addr)
			}

			if server.rateLimitConfig == nil {
				t.Error("server.rateLimitConfig is nil")
			}

			// If nil was passed, should use default
			if tt.rateLimitConfig == nil && server.rateLimitConfig == nil {
				t.Error("expected default rate limit config when nil is passed")
			}
		})
	}
}

// TestServerInitialState tests that a new server has correct initial state
func TestServerInitialState(t *testing.T) {
	t.Parallel()

	server := New(&ServerConfig{
		Addr:               ":8080",
		RateLimitConfig:    DefaultRateLimitConfig(),
		CheckOrigin:        nil,
		OnConnect:          nil,
		OnClientDisconnect: nil,
	})

	if server.running {
		t.Error("new server should not be running")
	}

	if server.addr != ":8080" {
		t.Errorf("server.addr = %v, want :8080", server.addr)
	}

	if server.upgrader.ReadBufferSize != 1024 {
		t.Errorf("upgrader.ReadBufferSize = %v, want 1024", server.upgrader.ReadBufferSize)
	}

	if server.upgrader.WriteBufferSize != 1024 {
		t.Errorf("upgrader.WriteBufferSize = %v, want 1024", server.upgrader.WriteBufferSize)
	}
}

// TestCheckOriginFunction tests custom origin checking
func TestCheckOriginFunction(t *testing.T) {
	t.Parallel()

	allowAll := func(r *http.Request) bool {
		return true
	}

	rejectAll := func(r *http.Request) bool {
		return false
	}

	tests := []struct {
		name        string
		checkOrigin CheckOriginFn
		wantNil     bool
	}{
		{
			name:        "allow all origins",
			checkOrigin: allowAll,
			wantNil:     false,
		},
		{
			name:        "reject all origins",
			checkOrigin: rejectAll,
			wantNil:     false,
		},
		{
			name:        "nil check origin",
			checkOrigin: nil,
			wantNil:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server := New(&ServerConfig{
				Addr:               ":8080",
				RateLimitConfig:    NoRateLimit(),
				CheckOrigin:        tt.checkOrigin,
				OnConnect:          nil,
				OnClientDisconnect: nil,
			})

			if tt.wantNil && server.upgrader.CheckOrigin != nil {
				t.Error("expected CheckOrigin to be nil")
			}

			if !tt.wantNil && server.upgrader.CheckOrigin == nil {
				t.Error("expected CheckOrigin to be non-nil")
			}
		})
	}
}

// TestRateLimitConfigConcurrency tests that rate limit config can be accessed concurrently
func TestRateLimitConfigConcurrency(t *testing.T) {
	t.Parallel()

	config := DefaultRateLimitConfig()

	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func() {
			_ = config.Enabled
			_ = config.MessagesPerSecond
			_ = config.Burst
			done <- true
		}()
	}

	for i := 0; i < 10; i++ {
		<-done
	}
}

// BenchmarkNewServer benchmarks server creation
func BenchmarkNewServer(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = New(&ServerConfig{
			Addr:               ":8080",
			RateLimitConfig:    DefaultRateLimitConfig(),
			CheckOrigin:        nil,
			OnConnect:          nil,
			OnClientDisconnect: nil,
		})
	}
}
