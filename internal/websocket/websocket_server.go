package websocket

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"runtime"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"golang.org/x/time/rate"

	"github.com/luciancaetano/knet"
	"github.com/luciancaetano/knet/internal/protocol"
)

// CheckOriginFn validates the origin of a WebSocket upgrade request.
// Return true to accept, false to reject.
type CheckOriginFn = func(r *http.Request) bool

// OnConnectFn is called after the WebSocket handshake completes and before the
// message-read loop starts.
//
// BREAKING CHANGE (HIGH-4 fix): the function now returns a bool.
// Return true to accept the connection; return false to reject it with a
// policy-violation close code.  This makes authentication enforceable at the
// API level rather than being an unenforced convention.
type OnConnectFn = func(client knet.Client) bool

// OnClientDisconnectFn is called when a client disconnects.
//
// voluntary is true when the remote peer closed the connection normally; false
// when the server closed it (rate-limit, protocol error, shutdown, etc.)
// (LOW-1 fix: previously always reported as voluntary).
type OnClientDisconnectFn = func(client knet.Client, voluntary bool)

// ServerConfig holds all configuration for a Server.
type ServerConfig struct {
	// Network address to listen on (e.g. ":8080").
	Addr string

	// Per-client rate limiting. nil → DefaultRateLimitConfig.
	RateLimitConfig *RateLimitConfig

	// CORS / origin validation for the WebSocket upgrade. nil → gorilla default.
	CheckOrigin CheckOriginFn

	// Connection lifecycle hooks. Both are optional (nil is valid).
	OnConnect          OnConnectFn
	OnClientDisconnect OnClientDisconnectFn

	// HIGH-1: maximum number of concurrent connections (0 = unlimited).
	MaxConnections int64

	// LOW-5: maximum concurrent connections from a single remote IP (0 = unlimited).
	MaxConnPerIP int64

	// HIGH-3: worker-pool size for handler goroutines (0 = GOMAXPROCS × 4).
	WorkerCount int

	// HIGH-3: depth of the worker task queue (0 = 10 000).
	WorkerQueueSize int

	// Idle read deadline; close the connection if no data arrives within this
	// window (0 = 60 s).
	ReadDeadline time.Duration

	// PERF-5: interval between keepalive pings sent to idle clients (0 = 54 s).
	PingInterval time.Duration

	// CRIT-3: TLS support.  Set TLSCertFile + TLSKeyFile to enable wss://.
	// TLSConfig is optional additional configuration (ciphers, client auth, …).
	TLSCertFile string
	TLSKeyFile  string
	TLSConfig   *tls.Config

	// Logger routes knet diagnostic output to your application's logging
	// infrastructure. nil → knet.DefaultLogger() (writes via slog.Default).
	// Use knet.NopLogger() to suppress all output.
	Logger knet.Logger
}

// RateLimitConfig defines per-client message rate limiting (token-bucket).
type RateLimitConfig struct {
	MessagesPerSecond rate.Limit
	Burst             int
	Enabled           bool
}

// DefaultRateLimitConfig returns 100 msg/s with a burst of 200.
func DefaultRateLimitConfig() *RateLimitConfig {
	return &RateLimitConfig{
		MessagesPerSecond: 100,
		Burst:             200,
		Enabled:           true,
	}
}

// NoRateLimit returns a configuration with rate limiting disabled.
func NoRateLimit() *RateLimitConfig {
	return &RateLimitConfig{Enabled: false}
}

// Server implements the knet.WebsocketServer interface.
type Server struct {
	addr            string
	server          *http.Server
	clients         sync.Map // map[string]*Client
	handlers        sync.Map // map[uint32]func(knet.Client, []byte)
	jsonRPCHandlers sync.Map // map[string]func(map[string]interface{}) (interface{}, error)

	rateLimitConfig *RateLimitConfig

	// HIGH-1 / LOW-5: connection limits.
	maxConnections int64
	maxConnPerIP   int64
	connCount      atomic.Int64
	perIPConns     sync.Map // map[string]*atomic.Int64

	// HIGH-3: bounded worker pool.
	workerCh        chan func()
	workerCount     int
	workerQueueSize int
	handlerWg       sync.WaitGroup // in-flight handler tasks
	clientWg        sync.WaitGroup // live handleClient goroutines

	// Timing.
	readDeadline time.Duration
	pingInterval time.Duration

	// CRIT-3: TLS.
	tlsCertFile string
	tlsKeyFile  string
	tlsConfig   *tls.Config

	mu           sync.RWMutex
	running      bool
	upgrader     websocket.Upgrader
	onConnect    OnConnectFn
	onDisconnect OnClientDisconnectFn
	log          knet.Logger
}

// New creates a new Server from the provided config.
func New(cfg *ServerConfig) *Server {
	if cfg.RateLimitConfig == nil {
		cfg.RateLimitConfig = DefaultRateLimitConfig()
	}

	readDeadline := cfg.ReadDeadline
	if readDeadline <= 0 {
		readDeadline = 60 * time.Second
	}
	pingInterval := cfg.PingInterval
	if pingInterval <= 0 {
		pingInterval = 54 * time.Second
	}
	workerCount := cfg.WorkerCount
	if workerCount <= 0 {
		workerCount = runtime.GOMAXPROCS(0) * 4
	}
	workerQueueSize := cfg.WorkerQueueSize
	if workerQueueSize <= 0 {
		workerQueueSize = 10_000
	}

	logger := cfg.Logger
	if logger == nil {
		logger = knet.DefaultLogger()
	}

	return &Server{
		addr:            cfg.Addr,
		rateLimitConfig: cfg.RateLimitConfig,
		maxConnections:  cfg.MaxConnections,
		maxConnPerIP:    cfg.MaxConnPerIP,
		workerCount:     workerCount,
		workerQueueSize: workerQueueSize,
		readDeadline:    readDeadline,
		pingInterval:    pingInterval,
		tlsCertFile:     cfg.TLSCertFile,
		tlsKeyFile:      cfg.TLSKeyFile,
		tlsConfig:       cfg.TLSConfig,
		onConnect:       cfg.OnConnect,
		onDisconnect:    cfg.OnClientDisconnect,
		log:             logger,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			CheckOrigin:     cfg.CheckOrigin,
		},
	}
}

// Start starts the WebSocket server.
//
// LOW-3 fix: uses net.Listen for synchronous bind-error detection instead of
// the previous 100 ms heuristic timer.
//
// MED-5 fix: HTTP server timeouts are configured to prevent Slowloris attacks
// on the upgrade handshake path.
func (s *Server) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return fmt.Errorf(knet.ErrServerAlreadyRunning)
	}
	s.running = true
	s.mu.Unlock()

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", s.handleWebSocket)

	// Bind synchronously so port conflicts surface immediately as an error
	// rather than after a 100 ms race-prone timer.
	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		s.mu.Lock()
		s.running = false
		s.mu.Unlock()
		return err
	}

	s.server = &http.Server{
		Addr:              s.addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second, // prevents Slowloris on the upgrade path
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	// Initialise and start the bounded worker pool (HIGH-3 fix).
	s.workerCh = make(chan func(), s.workerQueueSize)
	for i := 0; i < s.workerCount; i++ {
		go func() {
			for task := range s.workerCh {
				task()
			}
		}()
	}

	go func() {
		var serveErr error
		if s.tlsCertFile != "" || s.tlsConfig != nil {
			s.server.TLSConfig = s.tlsConfig
			serveErr = s.server.ServeTLS(ln, s.tlsCertFile, s.tlsKeyFile)
		} else {
			serveErr = s.server.Serve(ln)
		}
		if serveErr != nil && serveErr != http.ErrServerClosed {
			s.log.Error("server error", "error", serveErr)
		}
	}()

	// Watch for external context cancellation.
	go func() {
		<-ctx.Done()
		stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s.Stop(stopCtx) //nolint:errcheck
	}()

	return nil
}

// Stop gracefully shuts down the server.
//
// MED-3 fix: Stop now waits for all handleClient goroutines and in-flight
// handler tasks to finish before returning.  Callers can safely release
// application resources (DB connections, caches, …) immediately after Stop.
//
// Shutdown order:
//  1. Mark server as stopped; close all client connections.
//  2. Wait for handleClient goroutines to exit (no new tasks after this).
//  3. Wait for in-flight handler tasks.
//  4. Close the worker channel (drain + exit workers).
//  5. Shut down the HTTP listener.
func (s *Server) Stop(ctx context.Context) error {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return nil
	}
	s.running = false
	s.mu.Unlock()

	// Flag all clients as server-closed and close their connections.
	s.clients.Range(func(key, value interface{}) bool {
		if client, ok := value.(*Client); ok {
			client.serverClosed.Store(true)
			client.Close(ctx)
		}
		return true
	})

	// Wait for all handleClient goroutines to exit; after this point no new
	// handler tasks can be submitted to the pool.
	s.clientWg.Wait()

	// Wait for all already-dispatched handler tasks.
	s.handlerWg.Wait()

	// Shut down the worker pool.
	if s.workerCh != nil {
		close(s.workerCh)
		s.workerCh = nil
	}

	if s.server != nil {
		return s.server.Shutdown(ctx)
	}
	return nil
}

// RegisterHandler registers a handler for a specific command ID.
// The handler is executed in the server's worker pool.
func (s *Server) RegisterHandler(ctx context.Context, commandID uint32, handler knet.HandlerFunc) error {
	s.handlers.Store(commandID, handler)
	return nil
}

// RegisterJSONRPCHandler registers a JSON-RPC 2.0 method handler.
func (s *Server) RegisterJSONRPCHandler(ctx context.Context, method string, handler knet.JSONRPCHandler) error {
	s.jsonRPCHandlers.Store(method, handler)
	return nil
}

// handleWebSocket upgrades an HTTP connection to WebSocket and launches the
// per-client goroutine.
func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	// HIGH-1: global connection cap.
	if s.maxConnections > 0 && s.connCount.Load() >= s.maxConnections {
		http.Error(w, "server at capacity", http.StatusServiceUnavailable)
		return
	}

	// LOW-5: per-IP connection cap.
	ip := extractIP(r.RemoteAddr)
	if !s.incrIPCount(ip) {
		http.Error(w, "too many connections from this address", http.StatusTooManyRequests)
		return
	}

	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.decrIPCount(ip)
		return
	}

	// HIGH-2: enforce the protocol payload limit at the transport layer.
	// gorilla will close the connection with 1009 (Message Too Big) before
	// allocating a buffer for oversized frames, preventing memory exhaustion.
	conn.SetReadLimit(int64(protocol.MaxPayloadSize) + int64(protocol.HeaderSize))

	s.connCount.Add(1)
	client := NewClient(conn, r.RemoteAddr, s.rateLimitConfig, s.pingInterval)
	s.clients.Store(client.ID(), client)

	s.clientWg.Add(1)
	go func() {
		defer func() {
			s.clientWg.Done()
			s.connCount.Add(-1)
			s.decrIPCount(ip)
		}()
		s.handleClient(client)
	}()
}

// handleClient is the per-connection read loop.
func (s *Server) handleClient(client *Client) {
	defer func() {
		// LOW-1 fix: voluntary == true only when the remote peer initiated the
		// close; false when the server closed it.
		voluntary := !client.serverClosed.Load()
		if s.onDisconnect != nil {
			s.onDisconnect(client, voluntary)
		}
		s.clients.Delete(client.ID())
		client.Close(context.Background())
	}()

	client.conn.SetReadDeadline(time.Now().Add(s.readDeadline))
	client.conn.SetPongHandler(func(string) error {
		client.conn.SetReadDeadline(time.Now().Add(s.readDeadline))
		return nil
	})

	// HIGH-4 fix: onConnect returns bool; false = reject the connection.
	if s.onConnect != nil {
		if !s.onConnect(client) {
			client.serverClosed.Store(true)
			client.CloseWithCode(context.Background(), websocket.ClosePolicyViolation, "unauthorized")
			return
		}
	}

	// MED-2 fix: when the client context is cancelled, immediately set a past
	// deadline so a goroutine blocked in ReadMessage returns at once rather
	// than waiting up to readDeadline seconds.
	go func() {
		<-client.Context().Done()
		client.conn.SetReadDeadline(time.Now())
	}()

	for {
		_, data, err := client.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				s.log.Warn("unexpected close", "client_id", client.ID(), "error", err)
			}
			return
		}

		client.conn.SetReadDeadline(time.Now().Add(s.readDeadline))

		if !client.CheckRateLimit(context.Background()) {
			s.log.Warn("rate limit exceeded", "client_id", client.ID(), "addr", client.RemoteAddr())
			client.serverClosed.Store(true)
			client.CloseWithCode(context.Background(), websocket.ClosePolicyViolation, "rate limit exceeded")
			return
		}

		commandID, payload, err := protocol.Decode(data)
		if err != nil {
			client.serverClosed.Store(true)
			client.CloseWithCode(context.Background(), websocket.CloseProtocolError, knet.ErrInvalidMessageFormat)
			return
		}

		s.handleProtocolMessage(client, commandID, payload)
	}
}

// handleProtocolMessage routes a decoded message to its registered handler.
func (s *Server) handleProtocolMessage(client *Client, commandID uint32, payload []byte) {
	if commandID == knet.CmdJSONRPC {
		s.dispatchHandler(func() { s.handleJSONRPCMessage(client, payload) })
		return
	}

	if handler, ok := s.handlers.Load(commandID); ok {
		if handlerFunc, ok := handler.(knet.HandlerFunc); ok {
			s.dispatchHandler(func() { handlerFunc(client, payload) })
		}
	}
	// Unknown commands are silently ignored (fire-and-forget design).
}

// dispatchHandler submits fn to the worker pool.
//
// CRIT-2 fix: every handler runs inside a recover() so a panicking handler
// cannot crash the server process.
//
// MED-3 fix: handlerWg tracks every submitted task so Stop can wait for all
// in-flight work.
//
// HIGH-3 fix: the bounded worker pool prevents unbounded goroutine growth.
// If the queue is full the task is dropped (with a log) rather than spawning
// an unlimited number of goroutines.
func (s *Server) dispatchHandler(fn func()) {
	s.handlerWg.Add(1)

	task := func() {
		defer s.handlerWg.Done()
		defer func() {
			if r := recover(); r != nil {
				s.log.Error("handler panic recovered", "panic", r, "stack", string(debug.Stack()))
			}
		}()
		fn()
	}

	select {
	case s.workerCh <- task:
		// queued successfully
	default:
		// Worker queue is full; undo the Add so the WaitGroup stays consistent.
		s.handlerWg.Done()
		s.log.Warn("worker queue full, dropping message handler")
	}
}

// --- JSON-RPC -----------------------------------------------------------------

// JSONRPCRequest represents a JSON-RPC 2.0 request.
type JSONRPCRequest struct {
	JSONRPC string                 `json:"jsonrpc"`
	Method  string                 `json:"method"`
	Params  map[string]interface{} `json:"params,omitempty"`
	ID      interface{}            `json:"id"`
}

// JSONRPCResponse represents a JSON-RPC 2.0 response.
type JSONRPCResponse struct {
	JSONRPC string        `json:"jsonrpc"`
	Result  interface{}   `json:"result,omitempty"`
	Error   *JSONRPCError `json:"error,omitempty"`
	ID      interface{}   `json:"id"`
}

// JSONRPCError represents a JSON-RPC 2.0 error object.
type JSONRPCError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

func (s *Server) handleJSONRPCMessage(client *Client, payload []byte) {
	var req JSONRPCRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		s.sendJSONRPCError(client, nil, knet.JSONRPCParseError, knet.ErrParseError, nil)
		return
	}

	if req.JSONRPC != knet.JSONRPCVersion {
		s.sendJSONRPCError(client, req.ID, knet.JSONRPCInvalidRequest, knet.ErrInvalidRequest, nil)
		return
	}

	handler, ok := s.jsonRPCHandlers.Load(req.Method)
	if !ok {
		s.sendJSONRPCError(client, req.ID, knet.JSONRPCMethodNotFound, knet.ErrMethodNotFound, nil)
		return
	}

	handlerFunc, ok := handler.(knet.JSONRPCHandler)
	if !ok {
		s.sendJSONRPCError(client, req.ID, knet.JSONRPCInternalError, knet.ErrInternalError, nil)
		return
	}

	result, err := handlerFunc(req.Params)
	if err != nil {
		// MED-4 fix: log the full error server-side; send only the generic
		// "Internal error" string to the client to prevent information leakage.
		s.log.Error("JSON-RPC handler error", "method", req.Method, "error", err)
		s.sendJSONRPCError(client, req.ID, knet.JSONRPCInternalError, knet.ErrInternalError, nil)
		return
	}

	response := JSONRPCResponse{
		JSONRPC: knet.JSONRPCVersion,
		Result:  result,
		ID:      req.ID,
	}

	responseData, err := json.Marshal(response)
	if err != nil {
		s.sendJSONRPCError(client, req.ID, knet.JSONRPCInternalError, knet.ErrInternalError, nil)
		return
	}

	client.Send(context.Background(), knet.CmdJSONRPC, responseData) //nolint:errcheck
}

func (s *Server) sendJSONRPCError(client *Client, id interface{}, code int, message string, data interface{}) {
	response := JSONRPCResponse{
		JSONRPC: knet.JSONRPCVersion,
		Error:   &JSONRPCError{Code: code, Message: message, Data: data},
		ID:      id,
	}

	responseData, err := json.Marshal(response)
	if err != nil {
		s.log.Error("failed to marshal JSON-RPC error response", "error", err)
		return
	}

	if err := client.Send(context.Background(), knet.CmdJSONRPC, responseData); err != nil {
		s.log.Warn("failed to send JSON-RPC error to client", "client_id", client.ID(), "error", err)
	}
}

// --- Client helpers -----------------------------------------------------------

// GetClient returns a client by its UUID.
func (s *Server) GetClient(id string) (*Client, bool) {
	if client, ok := s.clients.Load(id); ok {
		return client.(*Client), true
	}
	return nil, false
}

// SendToClient sends a protocol message to a specific client.
func (s *Server) SendToClient(ctx context.Context, clientID string, commandID uint32, payload []byte) error {
	client, ok := s.GetClient(clientID)
	if !ok {
		return fmt.Errorf("%s: %s", knet.ErrClientNotFound, clientID)
	}
	return client.Send(ctx, commandID, payload)
}

// BroadcastCommand sends a command to all connected clients.
//
// PERF-1 fix: the payload is encoded once and the same bytes are delivered to
// every client's send channel, eliminating N redundant allocations.
//
// PERF-6 fix: per-client delivery is non-blocking (sendRaw); a slow client
// with a full buffer has its copy dropped rather than stalling the broadcast.
//
// LOW-7 fix: returns a non-nil error reporting how many clients were not
// reached (previously always returned nil).
func (s *Server) BroadcastCommand(ctx context.Context, commandID uint32, payload []byte) error {
	encoded, err := protocol.Encode(commandID, payload)
	if err != nil {
		return err
	}

	var failCount int
	s.clients.Range(func(key, value interface{}) bool {
		if client, ok := value.(*Client); ok {
			if err := client.sendRaw(ctx, encoded); err != nil {
				failCount++
			}
		}
		return true
	})

	if failCount > 0 {
		return fmt.Errorf("broadcast: failed to deliver to %d client(s)", failCount)
	}
	return nil
}

// --- Internal helpers ---------------------------------------------------------

// extractIP returns the host component of a "host:port" remote address.
func extractIP(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return remoteAddr
	}
	return host
}

// incrIPCount increments the connection counter for ip and returns false if
// the per-IP cap has been reached.
func (s *Server) incrIPCount(ip string) bool {
	if s.maxConnPerIP <= 0 {
		return true
	}
	val, _ := s.perIPConns.LoadOrStore(ip, new(atomic.Int64))
	counter := val.(*atomic.Int64)
	if counter.Add(1) > s.maxConnPerIP {
		counter.Add(-1)
		return false
	}
	return true
}

// decrIPCount decrements the connection counter for ip.
func (s *Server) decrIPCount(ip string) {
	if s.maxConnPerIP <= 0 {
		return
	}
	if val, ok := s.perIPConns.Load(ip); ok {
		val.(*atomic.Int64).Add(-1)
	}
}
