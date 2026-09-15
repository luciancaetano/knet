# 🚀 knet

[![Go Reference](https://pkg.go.dev/badge/github.com/luciancaetano/knet.svg)](https://pkg.go.dev/github.com/luciancaetano/knet)
[![Go Report Card](https://goreportcard.com/badge/github.com/luciancaetano/knet)](https://goreportcard.com/report/github.com/luciancaetano/knet)

A **high-performance** Go library for building game servers and real-time applications using WebSocket communication. Implements a command pattern protocol with efficient binary encoding and optional JSON-RPC 2.0 support.

## 📖 Overview

knet provides a robust WebSocket server framework designed for game servers and real-time applications that require:

- **🎯 Command Pattern Protocol**: Efficient binary protocol with 4-byte command IDs + binary payload
- **📡 JSON-RPC 2.0 Support**: Optional JSON-RPC handler registration for standard RPC workflows
- **🛡️ Per-Client Rate Limiting**: Token bucket algorithm to prevent DoS attacks
- **⚡ Zero-Copy Performance**: Payload slices reference the original buffer for maximum efficiency
- **🏭 Production-Ready**: Built-in timeouts, protections, and security features
- **🔌 Connection Callbacks**: Track connections with `OnConnect` callbacks
- **📊 Broadcasting**: Send messages to all connected clients efficiently

## ✨ Key Features

- 📦 **Lightweight binary protocol**: 4 bytes (CommandID uint32 big-endian) + binary payload
- 🎯 **Command pattern architecture** for extensible message handling
- 🔄 **Optional JSON-RPC 2.0 support** for standard RPC calls
- ⚙️ **Configuration-based API**: Use `ws.NewConfig()` for flexible server setup
- 🔌 **Connection lifecycle callbacks**: `OnConnect` and `OnDisconnect` for tracking clients
- 🛡️ **Per-client rate limiting** (token bucket algorithm)
- ⚡ **Zero-copy decode** (payload slices the original buffer — DO NOT modify)
- ⏱️ **Timeouts and protections** (read/write/pong, payload limits, race protections)
- 📊 **Broadcasting support** to send messages to all clients
- 🔐 **Origin validation** for CORS security

## 📦 Installation

```bash
go get github.com/luciancaetano/knet
```

**Dependencies:**
```bash
go get github.com/gorilla/websocket
go get golang.org/x/time/rate
go get github.com/google/uuid
```

Or use the module system (recommended):
```bash
go mod init your-project
go get github.com/luciancaetano/knet
```

## 🚀 Quick Start

```go
package main

import (
    "context"
    "log"
    "time"

    "github.com/luciancaetano/knet"
    "github.com/luciancaetano/knet/ws"
)

func main() {
    ctx := context.Background()

    // Create server configuration with callbacks
    config := ws.NewConfig(
        ":8080",                        // Server address
        ws.DefaultRateLimitConfig(),    // Rate limiting (100 msg/s, burst 200)
        ws.AllOrigins(),                // Origin check (use custom in production!)
        func(client knet.Client) { // OnConnect callback
            log.Printf("✅ Client connected: %s from %s", client.ID(), client.RemoteAddr())
            
            // Send welcome message
            welcomeMsg := []byte("Welcome to the server!")
            client.Send(ctx, 0x0001, welcomeMsg)
        },
        func(client knet.Client) { // OnDisconnect callback
            log.Printf("👋 Client disconnected: %s", client.ID())
        },
    )

    // Create server with configuration
    server := ws.New(config)

    // Register an async command handler (0x0100 = player login)
    // Handlers receive the client and payload, no return value (fire-and-forget)
    err := server.RegisterHandler(ctx, 0x0100, func(client knet.Client, payload []byte) {
        log.Printf("📨 Received login request: %s", string(payload))
        
        // Process the login
        response := []byte("Login successful")
        
        // Send response back to the client (optional)
        client.Send(ctx, 0x0100, response)
    })
    if err != nil {
        log.Fatal(err)
    }

    // Start the server
    log.Println("🚀 Starting server on :8080...")
    if err := server.Start(ctx); err != nil {
        log.Fatal(err)
    }

    // Graceful shutdown example
    time.Sleep(10 * time.Minute)
    log.Println("Shutting down...")
    _ = server.Stop(ctx)
}
```

## 📡 Protocol

### Command Pattern Binary Protocol

The library uses a lightweight binary protocol based on the command pattern:

**Message Format:**
```
┌────────────────┬──────────────────────┐
│  4 bytes       │  N bytes             │
│  CommandID     │  Payload             │
│  (uint32 BE)   │  (binary data)       │
└────────────────┴──────────────────────┘
```

**Example:** 
- CommandID `0x01` + payload `"hello"` 
- Bytes: `[0x00, 0x00, 0x00, 0x01, 'h', 'e', 'l', 'l', 'o']`

This command pattern allows you to register specific handlers for each command ID, making it easy to build game servers with different message types:
- `0x0100-0x01FF`: Player actions (movement, combat, etc.)
- `0x0200-0x02FF`: Chat messages
- `0x0300-0x03FF`: Inventory operations
- `0x0400-0x04FF`: Game state updates
- And so on...

### JSON-RPC 2.0 Support

In addition to the binary command protocol, the library provides optional JSON-RPC 2.0 support for standard RPC workflows. JSON-RPC messages use reserved command IDs and follow the [JSON-RPC 2.0 specification](https://www.jsonrpc.org/specification).

**Reserved Command IDs for JSON-RPC:**
- `0xFFFFFFFF`: JSON-RPC requests/responses
- `0xFFFFFFFE`: JSON-RPC error responses

**Available Command IDs for your application:** `0x00000000` through `0xFFFFFFFD`

## 🏗️ Architecture Features

- ⚡ **Ultra-efficient binary protocol** (4-byte overhead per message)
- 🔄 **Asynchronous command handlers** (fire-and-forget pattern, run in goroutines)
- 🔁 **Synchronous JSON-RPC handlers** (request-response pattern)
- 📋 **Zero-copy decode** (payload is a slice of the read buffer — DO NOT modify)
- 🛡️ **Per-client rate limiting** (token bucket algorithm)
- ⏱️ **Timeouts**: Read 60s (renewed on each message), Write 10s, Ping every 54s
- 📏 **Default max payload**: 10MB
- 🔌 **Connection callbacks**: Track client lifecycle with `OnConnect`
- 📊 **Broadcasting**: Send to all connected clients with `BroadcastCommand`
- 🔐 **Optional JSON-RPC 2.0 support** (via dedicated handlers)

## 🔧 Server Configuration Examples

### Default Rate Limiting
```go
// 100 messages/second, burst 200
config := ws.NewConfig(
    ":8080",
    ws.DefaultRateLimitConfig(),
    ws.AllOrigins(),
    nil,  // OnConnect callback (optional)
    nil,  // OnDisconnect callback (optional)
)
server := ws.New(config)
```

### No Rate Limiting
```go
config := ws.NewConfig(
    ":8080",
    ws.NoRateLimit(),
    ws.AllOrigins(),
    nil,
    nil,
)
server := ws.New(config)
```

### Custom Rate Limiting
```go
rateLimitConfig := &ws.RateLimitConfig{
    MessagesPerSecond: 50,  // 50 messages per second
    Burst:             100, // Allow bursts up to 100 messages
    Enabled:           true,
}
config := ws.NewConfig(":8080", rateLimitConfig, ws.AllOrigins(), nil, nil)
server := ws.New(config)
```

### Custom Origin Check (Production)
```go
checkOrigin := func(r *http.Request) bool {
    origin := r.Header.Get("Origin")
    allowedOrigins := []string{
        "https://yourdomain.com",
        "https://www.yourdomain.com",
    }
    for _, allowed := range allowedOrigins {
        if origin == allowed {
            return true
        }
    }
    return false
}
config := ws.NewConfig(":8080", ws.DefaultRateLimitConfig(), checkOrigin, nil, nil)
server := ws.New(config)
```

### With Connection Callbacks
```go
config := ws.NewConfig(
    ":8080",
    ws.DefaultRateLimitConfig(),
    ws.AllOrigins(),
    func(client knet.Client) {
        log.Printf("✅ Client connected: %s", client.ID())
        // Send welcome message, initialize state, etc.
    },
    func(client knet.Client) {
        log.Printf("👋 Client disconnected: %s", client.ID())
        // Cleanup resources, update metrics, etc.
    },
)
server := ws.New(config)
```

⚠️ **Warning**: Never use `ws.AllOrigins()` in production! It allows connections from any origin.

## 🔌 Connection Lifecycle

### OnConnect and OnDisconnect Callbacks

The new API provides two lifecycle callbacks:

- **OnConnect**: Called when a client successfully connects (after WebSocket handshake)
- **OnDisconnect**: Called when a client disconnects (connection closed or error)

```go
config := ws.NewConfig(
    ":8080",
    ws.DefaultRateLimitConfig(),
    ws.AllOrigins(),
    func(client knet.Client) {
        log.Printf("🎉 New connection: ID=%s, RemoteAddr=%s", client.ID(), client.RemoteAddr())
        
        // Send a welcome message
        welcomeMsg := []byte("Welcome to the server!")
        client.Send(context.Background(), 0x0001, welcomeMsg)
    },
    func(client knet.Client) {
        log.Printf("👋 Client disconnected: ID=%s", client.ID())
        
        // Cleanup resources, update metrics, broadcast to other clients, etc.
    },
)
server := ws.New(config)
```

**Common Use Cases:**

**OnConnect:**
- ✅ **Track connections**: Add client to a registry for broadcasting
- 💬 **Send welcome messages**: Greet the client or send initial state
- 🔐 **Authentication**: Verify credentials before accepting messages
- 🎮 **Initialize state**: Set up client-specific data structures
- 📊 **Metrics**: Track connection counts and rates

**OnDisconnect:**
- 🧹 **Cleanup resources**: Remove from registries, close channels, etc.
- 📊 **Update metrics**: Track connection duration, disconnection reasons
- 📢 **Notify others**: Broadcast to remaining clients about user leaving
- 💾 **Save state**: Persist client data before cleanup
- 🔍 **Audit logging**: Record connection lifecycle events

**Important Notes:**
- Both callbacks are optional (can be `nil`)
- OnConnect runs synchronously during connection setup
- OnDisconnect runs synchronously during connection teardown
- Avoid long-running operations that could block
- The client is already added to the server's internal client map in OnConnect
- The client is still in the server's map during OnDisconnect (removed after callback)

### Connection Tracking Example

Track all connected clients with automatic cleanup using OnDisconnect:

```go
var (
    clientsMu sync.RWMutex
    clients   = make(map[string]knet.Client)
)

config := ws.NewConfig(
    ":8080",
    ws.DefaultRateLimitConfig(),
    ws.AllOrigins(),
    func(client knet.Client) {
        // Add client to tracking map
        clientsMu.Lock()
        clients[client.ID()] = client
        count := len(clients)
        clientsMu.Unlock()
        
        log.Printf("✅ Client connected: %s (Total: %d)", client.ID(), count)
    },
    func(client knet.Client) {
        // Remove client from tracking map
        clientsMu.Lock()
        delete(clients, client.ID())
        count := len(clients)
        clientsMu.Unlock()
        
        log.Printf("👋 Client disconnected: %s (Total: %d)", client.ID(), count)
    },
)
server := ws.New(config)
```

## 🎯 Message Handlers

knet supports two communication patterns:

### 1. Command Pattern (Asynchronous - Fire-and-Forget)

Handlers are executed asynchronously. They receive the client and payload but don't return a response. The handler can optionally send messages back to the client or broadcast to all clients.

**Use this for:**
- 🎮 Game events (player movement, actions)
- 💬 Chat messages
- 📢 Notifications
- 🔄 State updates that don't require acknowledgment

```go
// Example: Player movement command (ID 0x0100)
server.RegisterHandler(ctx, 0x0100, func(client knet.Client, payload []byte) {
    if len(payload) == 0 {
        log.Println("Empty payload received")
        return
    }
    
    // Process movement data
    position := processMovement(payload)
    
    // Optionally send response back to this client
    client.Send(ctx, 0x0100, position)
    
    // Or broadcast to all clients
    server.BroadcastCommand(ctx, 0x0101, position)
})

// Example: Chat message command (ID 0x0200)
server.RegisterHandler(ctx, 0x0200, func(client knet.Client, payload []byte) {
    chatMsg := processChatMessage(payload)
    
    // Broadcast to all clients (including sender)
    server.BroadcastCommand(ctx, 0x0200, chatMsg)
})

// Example: Notification handler (ID 0x0300) - no response needed
server.RegisterHandler(ctx, 0x0300, func(client knet.Client, payload []byte) {
    log.Printf("Notification received from %s: %s", client.ID(), string(payload))
    // Just log it, no response needed
})
```

### 2. JSON-RPC Pattern (Synchronous - Request-Response)

For standard RPC-style communication where you need a response, use JSON-RPC handlers. These follow the request-response pattern.

**Use this for:**
- 🔍 Data queries (get player stats, inventory)
- ⚙️ Configuration requests
- 🔐 Authentication
- 📊 Any operation that requires a response

```go
// Register a JSON-RPC method for getting player stats
server.RegisterJSONRPCHandler(ctx, "player.getStats", func(params map[string]interface{}) (interface{}, error) {
    playerID := params["playerId"].(string)
    stats := getPlayerStats(playerID)
    return stats, nil
})

// Register a JSON-RPC calculation method
server.RegisterJSONRPCHandler(ctx, "math.add", func(params map[string]interface{}) (interface{}, error) {
    a := params["a"].(float64)
    b := params["b"].(float64)
    return a + b, nil
})

// Register a complex method with validation
server.RegisterJSONRPCHandler(ctx, "game.createRoom", func(params map[string]interface{}) (interface{}, error) {
    roomName, ok := params["roomName"].(string)
    if !ok || roomName == "" {
        return nil, fmt.Errorf("invalid room name")
    }
    
    maxPlayers, ok := params["maxPlayers"].(float64)
    if !ok || maxPlayers < 2 {
        return nil, fmt.Errorf("invalid max players")
    }
    
    room := createGameRoom(roomName, int(maxPlayers))
    return room, nil
})
```

**Important Notes:**
- ⚠️ **Command handlers** run in separate goroutines and don't block the read loop
- ⚠️ **Command handlers** are fire-and-forget (async) - use `client.Send()` to respond
- ⚠️ **JSON-RPC handlers** are synchronous and return responses automatically
- ⚠️ DO NOT modify the payload slice (zero-copy)
- ✅ Unknown command IDs are silently ignored (fire-and-forget pattern)
- ✅ Use command handlers for game events, chat, notifications
- ✅ Use JSON-RPC handlers for queries and operations that need responses

## 🛡️ Security & Limits

### Rate Limiting

Per-client rate limiting using token bucket algorithm:

- ✅ Implemented per-client (independent limits)
- 🔒 Clients exceeding limits receive close code `1008` (Policy Violation)
- ⚙️ Configure via `RateLimitConfig` when creating the server
- 📊 Default: 100 messages/second, burst 200
- 🚫 Can be disabled with `ws.NoRateLimit()`

When a client exceeds the rate limit:
1. Server logs: `"Rate limit exceeded for client client_id=xxx remote_addr=xxx"`
2. Connection is closed with code `1008` (Policy Violation)
3. Client receives the close reason: `"Rate limit exceeded"`

### Security Features

| Feature | Default | Description |
|---------|---------|-------------|
| **Max Payload** | 10MB | Prevents OOM attacks |
| **Read Timeout** | 60s | Renewed per message, prevents hanging |
| **Write Timeout** | 10s | Prevents slow clients from blocking |
| **Ping Interval** | 54s | Automatic keepalive |
| **Pong Handler** | Auto | Detects dead connections |
| **Origin Validation** | Custom | Configure via `CheckOriginFn` |
| **Rate Limiting** | 100 msg/s | Per-client DoS prevention |

### Origin Validation Example

**⚠️ NEVER use `ws.AllOrigins()` in production!**

```go
// Production-ready origin check
checkOrigin := func(r *http.Request) bool {
    origin := r.Header.Get("Origin")
    allowed := []string{
        "https://yourdomain.com",
        "https://www.yourdomain.com",
        "https://app.yourdomain.com",
    }
    for _, allowedOrigin := range allowed {
        if origin == allowedOrigin {
            return true
        }
    }
    return false
}
config := ws.NewConfig(":8080", ws.DefaultRateLimitConfig(), checkOrigin, nil, nil)
server := ws.New(config)
```

## 📁 Project Structure

```
go-kephas-net/
├── README.md                  # This file
├── LICENSE                    # MIT License
├── Makefile                   # Build commands
├── go.mod                     # Go module definition
├── doc.go                     # Package documentation
├── kephasnet.go              # Public interfaces (Server, Client)
├── commands.go               # Constants (command IDs, errors)
├── session.go                # SessionStore interface
├── logger.go, metrics.go     # Logger/Metrics interfaces + default no-op impls
│
├── internal/                 # Internal implementation (not part of public API)
│   ├── protocol/            
│   │   └── protocol.go       # Binary encoding/decoding (Encode/Decode)
│   └── websocket/
│       ├── websocket_server.go  # Server implementation
│       └── websocket_client.go  # Client implementation
│
├── ws/                       # Public factory package
│   └── server.go             # Factory functions (New, DefaultRateLimitConfig, etc.)
│
├── room/                     # Opt-in: named client groups (room.Room, room.New)
├── observer/                 # Opt-in: interest-managed broadcast (observer.Set, observer.NewSet)
├── syncvar/                  # Opt-in: dirty-tracked state sync (syncvar.SyncVar, syncvar.New)
├── timing/                   # Opt-in: tick scheduler + RTT ping (timing.TimeManager, timing.New)
│
├── examples/                 # Example applications
│   └── js-chat/              # JavaScript chat example
│       ├── main.go           # Go server
│       ├── index.html        # Chat UI
│       ├── kephas-client.js  # JS client library
│       └── go.mod
│
└── tests/                    # Test suites
    ├── unit/                 # Unit tests
    ├── e2e/                  # End-to-end tests
    └── stress/               # Stress/load tests
        └── README.md         # Stress testing guide
```

**Key Design Principles:**
- 📦 **Use `ws.New()` for server creation** (implementation is internal)
- 🔒 **Internal packages are not part of the public API**
- 📝 **Public interfaces defined in `knet.go`**
- 🏭 **Factory pattern for clean API surface**

## 🎮 Use Cases

This library is ideal for:

| Use Case | Why knet? |
|----------|---------------|
| **🎮 Game Servers** | Command pattern perfect for different message types (movement, combat, chat, inventory) |
| **📊 Real-time Dashboards** | Low-latency binary protocol for high-frequency updates |
| **🤝 Collaboration Tools** | Broadcasting support for multi-user interactions |
| **💹 Trading Platforms** | Efficient binary encoding for market data streams |
| **🏠 IoT Command & Control** | Command-based protocol ideal for device management |
| **🔄 Microservices** | Low-latency service-to-service communication |
| **🔀 Hybrid Systems** | Both binary commands and JSON-RPC for flexibility |

## 💡 Best Practices

### ✅ Do's

1. **Always use `ws.New()` for server creation** - implementation lives in `internal/`
2. **Register handlers before calling `Start()`** - handlers cannot be added after server starts
3. **Use context for cancellation/timeouts** - especially in handlers
4. **Design your command ID space thoughtfully**:
   - `0x0100-0x01FF`: Player actions
   - `0x0200-0x02FF`: Chat/social
   - `0x0300-0x03FF`: Inventory/items
   - `0x0400-0x04FF`: Game state
5. **Use binary commands for high-frequency messages** (game state updates)
6. **Use JSON-RPC for administrative operations** (configuration, stats)
7. **Configure rate limiting appropriately** for your use case
8. **Use custom `CheckOriginFn` in production** - never allow all origins
9. **Track client connections** using `OnConnect` callback
10. **Monitor logs** for "Rate limit exceeded" and other warnings

### ❌ Don'ts

1. **DO NOT modify the payload slice** returned by handlers (it's zero-copy)
2. **DO NOT use `ws.AllOrigins()` in production** - security risk
3. **DO NOT use reserved command IDs**:
   - `0xFFFFFFFF` - JSON-RPC requests
   - `0xFFFFFFFE` - JSON-RPC errors
4. **DO NOT perform long-running operations** in `OnConnect` callback
5. **DO NOT assume handler execution order** - they run concurrently
6. **DO NOT ignore rate limiting** - always configure appropriate limits

## 🚀 Advanced Examples

### Broadcasting to All Clients

```go
// Maintain a client registry using callbacks
var (
    clientsMu sync.RWMutex
    clients   = make(map[string]knet.Client)
)

// Track clients via OnConnect and OnDisconnect
config := ws.NewConfig(
    ":8080",
    ws.DefaultRateLimitConfig(),
    ws.AllOrigins(),
    func(client knet.Client) {
        clientsMu.Lock()
        clients[client.ID()] = client
        clientsMu.Unlock()
    },
    func(client knet.Client) {
        clientsMu.Lock()
        delete(clients, client.ID())
        clientsMu.Unlock()
    },
)
server := ws.New(config)

// Manual broadcast function
func broadcastToAll(cmdID uint32, payload []byte) {
    clientsMu.RLock()
    defer clientsMu.RUnlock()
    
    for _, client := range clients {
        if client.IsAlive() {
            go client.Send(context.Background(), cmdID, payload)
        }
    }
}

// Or use built-in broadcast
server.BroadcastCommand(ctx, 0x0200, []byte("Server announcement"))
```

### Handler with Timeout

```go
server.RegisterHandler(ctx, 0x0500, func(client knet.Client, payload []byte) {
    // Create context with timeout for this handler
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    
    // Process with timeout
    result, err := processWithTimeout(ctx, payload)
    if err != nil {
        if ctx.Err() == context.DeadlineExceeded {
            errorMsg := []byte("operation timed out")
            client.Send(ctx, 0x0500, errorMsg)
        }
        return
    }
    
    // Send result back to client
    client.Send(ctx, 0x0500, result)
})
```

### Advanced Connection Tracking

```go
type ClientInfo struct {
    Client      knet.Client
    Username    string
    ConnectedAt time.Time
    LastSeen    time.Time
}

var (
    clientsMu    sync.RWMutex
    clientsInfo  = make(map[string]*ClientInfo)
)

config := ws.NewConfig(
    ":8080",
    ws.DefaultRateLimitConfig(),
    ws.AllOrigins(),
    func(client knet.Client) {
        info := &ClientInfo{
            Client:      client,
            ConnectedAt: time.Now(),
            LastSeen:    time.Now(),
        }
        
        clientsMu.Lock()
        clientsInfo[client.ID()] = info
        clientsMu.Unlock()
        
        log.Printf("Client %s connected", client.ID())
    },
    func(client knet.Client) {
        clientsMu.Lock()
        info, exists := clientsInfo[client.ID()]
        delete(clientsInfo, client.ID())
        clientsMu.Unlock()
        
        if exists {
            log.Printf("Client %s disconnected after %v", 
                client.ID(), time.Since(info.ConnectedAt))
        }
    },
)
server := ws.New(config)

// Update last seen timestamp
func updateLastSeen(clientID string) {
    clientsMu.Lock()
    defer clientsMu.Unlock()
    
    if info, exists := clientsInfo[clientID]; exists {
        info.LastSeen = time.Now()
    }
}
```

### Graceful Shutdown

```go
func main() {
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()
    
    config := ws.NewConfig(
        ":8080",
        ws.DefaultRateLimitConfig(),
        ws.AllOrigins(),
        nil,
        func(client knet.Client) {
            log.Printf("Client %s disconnected during shutdown", client.ID())
        },
    )
    server := ws.New(config)
    
    // Register handlers...
    server.RegisterHandler(ctx, 0x01, func(client knet.Client, payload []byte) {
        // Handle message
        log.Printf("Received from %s: %s", client.ID(), string(payload))
    })
    
    // Start server in goroutine
    go func() {
        if err := server.Start(ctx); err != nil {
            log.Printf("Server error: %v", err)
        }
    }()
    
    // Wait for interrupt signal
    sigChan := make(chan os.Signal, 1)
    signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
    <-sigChan
    
    log.Println("Shutting down gracefully...")
    
    // Give clients 5 seconds to disconnect
    shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer shutdownCancel()
    
    if err := server.Stop(shutdownCtx); err != nil {
        log.Printf("Error during shutdown: %v", err)
    }
    
    log.Println("Server stopped")
}
```

## 💬 Running the Chat Example

A complete, production-ready chat application example is available in the `examples/js-chat` directory. It demonstrates real-world usage with both Go server and JavaScript client.

### 🌟 Features

- ✅ Real-time chat messaging
- 👤 User authentication
- 📢 Message broadcasting
- 🎨 Modern responsive UI
- 📚 JavaScript client library (`kephas-client.js`)
- 🔄 Auto-reconnection on connection loss
- 🛡️ Rate limiting protection

### 🚀 Quick Start

1. **Navigate to the example directory:**
   ```bash
   cd examples/js-chat
   ```

2. **Install dependencies:**
   ```bash
   go mod download
   ```

3. **Start the server:**
   ```bash
   go run main.go
   ```
   
   The server will start on:
   - WebSocket: `ws://localhost:8080/ws`
   - HTTP/Static: `http://localhost:3000`

4. **Open the chat client:**
   - Navigate to `http://localhost:3000` in your browser
   - Enter a username and click "Connect"
   - Open multiple browser tabs/windows to test multi-user chat

### 📁 Example Structure

```
examples/js-chat/
├── main.go              # Go WebSocket server with chat logic
├── index.html           # Chat UI (HTML/CSS/JS)
├── kephas-client.js     # JavaScript client library
├── go.mod               # Go module dependencies
└── go.sum               # Dependency checksums
```

### 🔍 Key Implementation Details

The chat example demonstrates:

| Feature | Command ID | Description |
|---------|-----------|-------------|
| **Chat Messages** | `0x0001` | JSON messages with username, text, and timestamp |
| **User Joined** | `0x0003` | Notification when a user connects |
| **User Left** | `0x0004` | Notification when a user disconnects |
| **Get Users** | `0x0005` | Request list of online users |
| **Users List** | `0x0006` | Response with online users array |

**Server Implementation Highlights:**
- Asynchronous command-based message routing (fire-and-forget)
- Client tracking with connection callbacks
- Broadcasting to all connected clients
- Graceful disconnect handling
- Rate limiting (100 msg/s per client)

**Client Implementation Highlights:**
- Binary protocol encoding/decoding
- Asynchronous message handlers (fire-and-forget pattern)
- Synchronous JSON-RPC support for request-response
- Automatic reconnection logic
- Event-driven message handling
- Clean separation of concerns

You can use this example as a **starting point** for building your own real-time applications! 🚀

## 🌐 JavaScript Client Usage

The `kephas-client.js` library provides a browser-based client for connecting to knet servers. It supports both the asynchronous command pattern and synchronous JSON-RPC calls.

### Communication Patterns

#### 1. Asynchronous Commands (Fire-and-Forget)

Use for game events, chat messages, notifications, and any communication that doesn't require a response:

```javascript
// Create client instance
const client = new KephasClient({
  url: 'ws://localhost:8080/ws',
  autoReconnect: true,
  debug: true
});

// Register handler for incoming messages (async pattern)
client.on(0x0001, async (payload) => {
  const decoder = new TextDecoder();
  const message = decoder.decode(payload);
  console.log('Received:', message);
  
  // Optionally send another message (not a response)
  await client.sendString(0x0001, 'Thanks!');
});

// Connect to server
await client.connect();

// Send message (no response expected)
await client.sendString(0x0001, 'Hello, server!');
await client.sendJSON(0x0001, { type: 'chat', message: 'Hi!' });
```

#### 2. JSON-RPC (Request-Response)

Use for queries, operations that need confirmation, or when you need a response:

```javascript
// Call a JSON-RPC method and wait for response
const response = await client.sendJSONRPC('player.getStats', { 
  playerId: 123 
});
console.log('Player stats:', response.result);

// Another example
const result = await client.sendJSONRPC('math.add', { 
  a: 5, 
  b: 3 
});
console.log('Sum:', result.result); // 8
```

### Client Configuration Options

```javascript
const client = new KephasClient({
  url: 'ws://localhost:8080/ws',    // WebSocket URL
  autoReconnect: true,               // Auto-reconnect on disconnect
  reconnectDelay: 3000,              // Initial reconnect delay (ms)
  maxReconnectAttempts: Infinity,    // Max reconnection attempts
  connectionTimeout: 10000,          // Connection timeout (ms)
  debug: false                       // Enable debug logging
});
```

### API Reference

| Method | Description |
|--------|-------------|
| `connect()` | Connect to the server (returns Promise) |
| `disconnect()` | Disconnect from the server |
| `on(commandId, handler)` | Register async handler for command |
| `off(commandId)` | Unregister handler |
| `send(commandId, payload)` | Send binary data (fire-and-forget) |
| `sendString(commandId, text)` | Send text data (fire-and-forget) |
| `sendJSON(commandId, data)` | Send JSON data (fire-and-forget) |
| `sendJSONRPC(method, params, id)` | Send JSON-RPC request (returns Promise) |
| `isConnected()` | Check connection status |
| `getState()` | Get connection state |

### Complete Example

```html
<!DOCTYPE html>
<html>
<head>
    <title>knet Client</title>
    <script src="kephas-client.js"></script>
</head>
<body>
    <script>
        const client = new KephasClient({
            url: 'ws://localhost:8080/ws',
            debug: true
        });

        // Handle chat messages (async)
        client.on(0x0001, async (payload) => {
            const decoder = new TextDecoder();
            const data = JSON.parse(decoder.decode(payload));
            console.log(`${data.username}: ${data.message}`);
        });

        // Connect and send message
        (async () => {
            await client.connect();
            console.log('Connected!');
            
            // Send chat message (fire-and-forget)
            await client.sendJSON(0x0001, {
                username: 'John',
                message: 'Hello, everyone!'
            });
            
            // Or use JSON-RPC for request-response
            const stats = await client.sendJSONRPC('player.getStats', {
                playerId: 123
            });
            console.log('Stats:', stats.result);
        })();
    </script>
</body>
</html>
```

## 🧪 Testing

The project includes comprehensive test suites to ensure reliability and performance.

### Running Tests

```bash
# Run all tests
make test

# Or using go test directly
go test ./tests/... -v

# Run unit tests only
go test ./tests/unit/... -v

# Run end-to-end tests
go test ./tests/e2e/... -v

# Run with coverage
go test ./tests/... -cover -coverprofile=coverage.out
go tool cover -html=coverage.out
```

### Stress Tests

Stress tests validate performance under high load conditions. See [`tests/stress/README.md`](tests/stress/README.md) for detailed information. For stress/e2e tests that run against the server in a real Docker container (isolated process, real network), see [`docs/testing.md`](docs/testing.md).

**Quick stress test:**
```bash
cd tests/stress

# May need to increase file descriptor limit
ulimit -n 65536

# Run all stress tests (can take 15-30 minutes)
go test -v -timeout 30m

# Run specific stress test
go test -v -run TestStress5000Connections -timeout 10m
```

**Stress test scenarios:**
- ✅ 5,000 simultaneous connections
- ✅ 10,000 simultaneous connections
- ✅ 100,000 messages (100 clients × 1,000 messages)
- ✅ Message latency and throughput benchmarks

## 🤝 Contributing

Contributions are welcome! Here's how you can help:

### How to Contribute

1. **Fork the repository**
2. **Create a feature branch**
   ```bash
   git checkout -b feature/amazing-feature
   ```
3. **Make your changes**
   - Write tests for new features
   - Update documentation
   - Follow existing code style
4. **Run tests**
   ```bash
   make test
   ```
5. **Commit your changes**
   ```bash
   git commit -m "Add amazing feature"
   ```
6. **Push to the branch**
   ```bash
   git push origin feature/amazing-feature
   ```
7. **Open a Pull Request**

### Development Guidelines

- ✅ Write tests for all new features
- ✅ Update documentation (README, godoc comments)
- ✅ Follow Go best practices and conventions
- ✅ Keep the public API minimal and clean
- ✅ Add examples for new features
- ✅ Ensure all tests pass before submitting PR

### Reporting Issues

Found a bug? Have a feature request? Please [open an issue](https://github.com/luciancaetano/knet/issues) with:
- Clear description of the problem/feature
- Steps to reproduce (for bugs)
- Expected vs actual behavior
- Go version and OS
- Code samples if applicable

## 📝 License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## 🙏 Acknowledgments

Built with these excellent libraries:
- [Gorilla WebSocket](https://github.com/gorilla/websocket) - WebSocket implementation
- [golang.org/x/time/rate](https://pkg.go.dev/golang.org/x/time/rate) - Rate limiting
- [google/uuid](https://github.com/google/uuid) - UUID generation

## 📚 Additional Resources

- [📖 Go Package Documentation](https://pkg.go.dev/github.com/luciancaetano/knet)
- [🐛 Issue Tracker](https://github.com/luciancaetano/knet/issues)
- [💬 Discussions](https://github.com/luciancaetano/knet/discussions)
- [📋 Changelog](https://github.com/luciancaetano/knet/releases)

## ⭐ Support

If you find this library useful, please consider giving it a star on GitHub! ⭐

---

**Made with ❤️ for the Go community**
