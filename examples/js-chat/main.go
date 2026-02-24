package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/luciancaetano/knet"
	"github.com/luciancaetano/knet/ws"
)

const (
	// Command IDs for chat operations
	ChatMessageCommand uint32 = 0x0001
	BroadcastCommand   uint32 = 0x0002
	UserJoinedCommand  uint32 = 0x0003
	UserLeftCommand    uint32 = 0x0004
	GetUsersCommand    uint32 = 0x0005
	UsersListCommand   uint32 = 0x0006
	UserInfoCommand    uint32 = 0x0008
)

type ChatMessage struct {
	Username  string    `json:"username"`
	Message   string    `json:"message"`
	Timestamp time.Time `json:"timestamp"`
}

type UserInfo struct {
	ID       string    `json:"id"`
	Username string    `json:"username"`
	JoinedAt time.Time `json:"joinedAt"`
}

type ChatServer struct {
	server     knet.Server
	clients    map[string]*UserInfo
	clientsMux sync.RWMutex
	ctx        context.Context
}

func NewChatServer(addr string) *ChatServer {
	cs := &ChatServer{
		clients: make(map[string]*UserInfo),
		ctx:     context.Background(),
	}

	// Create WebSocket server with rate limiting and connection callback
	rateLimitConfig := ws.DefaultRateLimitConfig()
	cs.server = ws.New(ws.NewConfig(addr, rateLimitConfig, ws.AllOrigins(), func(client knet.Client) bool {
		log.Printf("New client connected: ID=%s, RemoteAddr=%s", client.ID(), client.RemoteAddr())

		cs.clientsMux.Lock()
		cs.clients[client.ID()] = &UserInfo{
			ID:       client.ID(),
			Username: "Guest_" + client.ID()[:8],
			JoinedAt: time.Now(),
		}
		cs.clientsMux.Unlock()
		return true // accept all connections (add auth logic here in production)
	}, func(client knet.Client, voluntary bool) {
		log.Printf("Client disconnected: ID=%s, RemoteAddr=%s, Voluntary=%v", client.ID(), client.RemoteAddr(), voluntary)

		cs.clientsMux.Lock()
		userInfo := cs.clients[client.ID()]
		delete(cs.clients, client.ID())
		cs.clientsMux.Unlock()

		// Broadcast user left
		if userInfo != nil {
			data, _ := json.Marshal(userInfo)
			cs.server.BroadcastCommand(context.Background(), UserLeftCommand, data)
		}
	}))

	return cs
}

func (cs *ChatServer) Start(ctx context.Context) error {
	// Register handlers
	if err := cs.server.RegisterHandler(ctx, ChatMessageCommand, cs.handleChatMessage); err != nil {
		return fmt.Errorf("failed to register chat message handler: %w", err)
	}

	if err := cs.server.RegisterHandler(ctx, GetUsersCommand, cs.handleGetUsers); err != nil {
		return fmt.Errorf("failed to register get users handler: %w", err)
	}

	if err := cs.server.RegisterHandler(ctx, UserInfoCommand, cs.handleUserInfo); err != nil {
		return fmt.Errorf("failed to register user info handler: %w", err)
	}

	// Start the server
	log.Printf("Starting WebSocket server on %s", ":8080")
	return cs.server.Start(ctx)
}

func (cs *ChatServer) handleChatMessage(client knet.Client, payload []byte) {
	var msg ChatMessage
	if err := json.Unmarshal(payload, &msg); err != nil {
		log.Printf("Invalid message format: %v", err)
		return
	}

	msg.Timestamp = time.Now()
	log.Printf("Message from %s: %s", msg.Username, msg.Message)

	// Prepare the response
	responseData, err := json.Marshal(msg)
	if err != nil {
		log.Printf("Failed to marshal message: %v", err)
		return
	}

	// Broadcast to all clients
	if err := cs.server.BroadcastCommand(cs.ctx, ChatMessageCommand, responseData); err != nil {
		log.Printf("Failed to broadcast message: %v", err)
	}
}

func (cs *ChatServer) handleGetUsers(client knet.Client, payload []byte) {
	cs.clientsMux.RLock()
	defer cs.clientsMux.RUnlock()

	users := make([]UserInfo, 0, len(cs.clients))
	for _, user := range cs.clients {
		users = append(users, *user)
	}

	data, err := json.Marshal(users)
	if err != nil {
		log.Printf("Failed to marshal users list: %v", err)
		return
	}

	// Send users list back to the requesting client
	if err := client.Send(cs.ctx, UsersListCommand, data); err != nil {
		log.Printf("Failed to send users list: %v", err)
	}
}

func (cs *ChatServer) handleUserInfo(client knet.Client, payload []byte) {
	var userInfo struct {
		Username string `json:"username"`
	}

	if err := json.Unmarshal(payload, &userInfo); err != nil {
		log.Printf("Invalid user info format: %v", err)
		return
	}

	cs.clientsMux.Lock()
	if user, exists := cs.clients[client.ID()]; exists {
		user.Username = userInfo.Username
		log.Printf("User %s set username to: %s", client.ID(), userInfo.Username)

		// Broadcast user joined with actual username
		data, _ := json.Marshal(user)
		cs.clientsMux.Unlock()
		cs.server.BroadcastCommand(cs.ctx, UserJoinedCommand, data)
	} else {
		cs.clientsMux.Unlock()
	}
}

func (cs *ChatServer) AddUser(clientID, username string) {
	cs.clientsMux.Lock()
	defer cs.clientsMux.Unlock()

	cs.clients[clientID] = &UserInfo{
		ID:       clientID,
		Username: username,
		JoinedAt: time.Now(),
	}

	log.Printf("User joined: %s (%s)", username, clientID)
}

func (cs *ChatServer) RemoveUser(clientID string) {
	cs.clientsMux.Lock()
	defer cs.clientsMux.Unlock()

	if user, exists := cs.clients[clientID]; exists {
		log.Printf("User left: %s (%s)", user.Username, clientID)
		delete(cs.clients, clientID)
	}
}

func main() {
	ctx := context.Background()

	// Create chat server
	chatServer := NewChatServer(":8080")

	// HTTP server for static files
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.ServeFile(w, r, "index.html")
			return
		}
		http.NotFound(w, r)
	})

	http.HandleFunc("/kephas-client.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		http.ServeFile(w, r, "kephas-client.js")
	})

	// Start HTTP server in a goroutine
	go func() {
		log.Println("Starting HTTP server on :3000")
		if err := http.ListenAndServe(":3000", nil); err != nil {
			log.Fatalf("HTTP server error: %v", err)
		}
	}()

	// Start WebSocket server (this blocks)
	log.Println("Starting servers... Press Ctrl+C to stop")
	if err := chatServer.Start(ctx); err != nil {
		log.Fatalf("Failed to start chat server: %v", err)
	}

	// Keep the server running
	select {}
}
