package stress_test

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/luciancaetano/knet"
	"github.com/luciancaetano/knet/ws"
)

const (
	ChatMessageCommand = 0x0001
	testServerAddr     = "localhost:8765"
)

type ChatMessage struct {
	Username  string    `json:"username"`
	Message   string    `json:"message"`
	Timestamp time.Time `json:"timestamp"`
}

// startTestServer starts a simple chat server for stress testing
func startTestServer(t *testing.T, ctx context.Context) knet.Server {
	rateLimitConfig := &ws.RateLimitConfig{
		MessagesPerSecond: 1000,
		Burst:             2000,
		Enabled:           true,
	}

	// Track clients
	var clientsMu sync.RWMutex
	clients := make(map[string]knet.Client)

	server := ws.New(ws.NewConfig(testServerAddr, rateLimitConfig, ws.AllOrigins(), func(client knet.Client) bool {
		// Add client to tracking map
		clientsMu.Lock()
		clients[client.ID()] = client
		clientsMu.Unlock()

		// Remove client when disconnected
		go func() {
			<-client.Context().Done()
			clientsMu.Lock()
			delete(clients, client.ID())
			clientsMu.Unlock()
		}()

		return true
	}, nil))

	// Register chat message handler
	err := server.RegisterHandler(ctx, ChatMessageCommand, func(client knet.Client, payload []byte) {
		var msg ChatMessage
		if err := json.Unmarshal(payload, &msg); err != nil {
			return
		}

		msg.Timestamp = time.Now()

		// Broadcast to all clients
		broadcastPayload, _ := json.Marshal(msg)
		clientsMu.RLock()
		for _, c := range clients {
			if c.IsAlive() {
				_ = c.Send(context.Background(), ChatMessageCommand, broadcastPayload)
			}
		}
		clientsMu.RUnlock()
	})

	if err != nil {
		t.Fatalf("Failed to register handler: %v", err)
	}

	go func() {
		if err := server.Start(ctx); err != nil && ctx.Err() == nil {
			t.Errorf("Server error: %v", err)
		}
	}()

	// Wait for server to start
	time.Sleep(500 * time.Millisecond)

	return server
}

// TestStress5000Connections tests 5000 simultaneous connections
func TestStress5000Connections(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stress test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	server := startTestServer(t, ctx)
	defer server.Stop(ctx)

	const numClients = 5000
	const messagesPerClient = 5

	var (
		connectedClients  int64
		failedConnections int64
		messagesSent      int64
		messagesReceived  int64
		totalLatency      int64
		wg                sync.WaitGroup
	)

	startTime := time.Now()

	// Create clients
	for i := 0; i < numClients; i++ {
		wg.Add(1)
		go func(clientID int) {
			defer wg.Done()

			// Connect with timeout
			dialCtx, dialCancel := context.WithTimeout(ctx, 10*time.Second)
			defer dialCancel()

			url := fmt.Sprintf("ws://%s/ws", testServerAddr)
			conn, _, err := websocket.DefaultDialer.DialContext(dialCtx, url, nil)
			if err != nil {
				atomic.AddInt64(&failedConnections, 1)
				return
			}
			defer conn.Close()

			atomic.AddInt64(&connectedClients, 1)

			// Set read deadline
			conn.SetReadDeadline(time.Now().Add(30 * time.Second))

			// Message handler goroutine
			msgReceived := make(chan struct{}, messagesPerClient*2)
			go func() {
				for {
					_, message, err := conn.ReadMessage()
					if err != nil {
						return
					}
					atomic.AddInt64(&messagesReceived, 1)
					select {
					case msgReceived <- struct{}{}:
					default:
					}
					_ = message
				}
			}()

			// Send messages
			for j := 0; j < messagesPerClient; j++ {
				msg := ChatMessage{
					Username: fmt.Sprintf("user_%d", clientID),
					Message:  fmt.Sprintf("Message %d from client %d", j, clientID),
				}

				payload, _ := json.Marshal(msg)

				// Encode message with command ID (4 bytes big-endian + payload)
				header := []byte{0x00, 0x00, 0x00, 0x01}
				fullMessage := append(header, payload...)

				sendStart := time.Now()
				if err := conn.WriteMessage(websocket.BinaryMessage, fullMessage); err != nil {
					return
				}
				atomic.AddInt64(&messagesSent, 1)

				// Wait for response (with timeout)
				select {
				case <-msgReceived:
					latency := time.Since(sendStart).Microseconds()
					atomic.AddInt64(&totalLatency, latency)
				case <-time.After(5 * time.Second):
					// Timeout waiting for message
				case <-ctx.Done():
					return
				}

				// Small delay between messages
				time.Sleep(10 * time.Millisecond)
			}

			// Keep connection alive for a bit
			time.Sleep(2 * time.Second)
		}(i)

		// Stagger connection attempts
		if i%100 == 0 && i > 0 {
			time.Sleep(100 * time.Millisecond)
		}
	}

	// Wait for all clients to finish
	wg.Wait()

	duration := time.Since(startTime)

	// Calculate statistics
	avgLatency := int64(0)
	if messagesSent > 0 {
		avgLatency = totalLatency / messagesSent
	}

	successRate := float64(connectedClients) / float64(numClients) * 100
	messageSuccessRate := float64(messagesReceived) / float64(messagesSent) * 100

	// Report results
	log.Printf("\n=== Stress Test Results ===")
	log.Printf("Duration: %v", duration)
	log.Printf("Target Clients: %d", numClients)
	log.Printf("Connected Clients: %d (%.2f%%)", connectedClients, successRate)
	log.Printf("Failed Connections: %d", failedConnections)
	log.Printf("Messages Sent: %d", messagesSent)
	log.Printf("Messages Received: %d (%.2f%%)", messagesReceived, messageSuccessRate)
	log.Printf("Average Latency: %d μs (%.2f ms)", avgLatency, float64(avgLatency)/1000.0)
	log.Printf("Messages/sec: %.2f", float64(messagesSent)/duration.Seconds())

	// Assertions
	if connectedClients < int64(numClients*0.95) {
		t.Errorf("Too many failed connections: %d/%d (%.2f%% success rate)",
			connectedClients, numClients, successRate)
	}

	if messagesReceived < messagesSent/2 {
		t.Errorf("Too many lost messages: %d sent, %d received (%.2f%% success rate)",
			messagesSent, messagesReceived, messageSuccessRate)
	}
}

// TestStress10000Connections tests 10000 simultaneous connections (more extreme)
func TestStress10000Connections(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping extreme stress test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	server := startTestServer(t, ctx)
	defer server.Stop(ctx)

	const numClients = 10000
	const messagesPerClient = 3

	var (
		connectedClients  int64
		failedConnections int64
		messagesSent      int64
		wg                sync.WaitGroup
	)

	startTime := time.Now()

	// Create clients
	for i := 0; i < numClients; i++ {
		wg.Add(1)
		go func(clientID int) {
			defer wg.Done()

			dialCtx, dialCancel := context.WithTimeout(ctx, 15*time.Second)
			defer dialCancel()

			url := fmt.Sprintf("ws://%s/ws", testServerAddr)
			conn, _, err := websocket.DefaultDialer.DialContext(dialCtx, url, nil)
			if err != nil {
				atomic.AddInt64(&failedConnections, 1)
				return
			}
			defer conn.Close()

			atomic.AddInt64(&connectedClients, 1)

			// Send a few messages
			for j := 0; j < messagesPerClient; j++ {
				msg := ChatMessage{
					Username: fmt.Sprintf("user_%d", clientID),
					Message:  fmt.Sprintf("Message %d", j),
				}

				payload, _ := json.Marshal(msg)
				header := []byte{0x00, 0x00, 0x00, 0x01}
				fullMessage := append(header, payload...)

				if err := conn.WriteMessage(websocket.BinaryMessage, fullMessage); err != nil {
					return
				}
				atomic.AddInt64(&messagesSent, 1)
				time.Sleep(50 * time.Millisecond)
			}

			// Keep connection alive
			time.Sleep(3 * time.Second)
		}(i)

		// More aggressive staggering for 10k connections
		if i%50 == 0 && i > 0 {
			time.Sleep(50 * time.Millisecond)
		}
	}

	wg.Wait()
	duration := time.Since(startTime)

	successRate := float64(connectedClients) / float64(numClients) * 100

	log.Printf("\n=== Extreme Stress Test Results ===")
	log.Printf("Duration: %v", duration)
	log.Printf("Target Clients: %d", numClients)
	log.Printf("Connected Clients: %d (%.2f%%)", connectedClients, successRate)
	log.Printf("Failed Connections: %d", failedConnections)
	log.Printf("Messages Sent: %d", messagesSent)
	log.Printf("Connections/sec: %.2f", float64(connectedClients)/duration.Seconds())

	// More lenient assertions for extreme test
	if connectedClients < int64(numClients*0.90) {
		t.Errorf("Too many failed connections: %d/%d (%.2f%% success rate)",
			connectedClients, numClients, successRate)
	}
}

// TestStressConcurrentMessaging tests heavy concurrent messaging
func TestStressConcurrentMessaging(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stress test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	server := startTestServer(t, ctx)
	defer server.Stop(ctx)

	const numClients = 100
	const messagesPerClient = 1000

	var (
		messagesSent     int64
		messagesReceived int64
		wg               sync.WaitGroup
	)

	startTime := time.Now()

	// Create clients that send many messages
	for i := 0; i < numClients; i++ {
		wg.Add(1)
		go func(clientID int) {
			defer wg.Done()

			url := fmt.Sprintf("ws://%s/ws", testServerAddr)
			conn, _, err := websocket.DefaultDialer.Dial(url, nil)
			if err != nil {
				t.Errorf("Failed to connect: %v", err)
				return
			}
			defer conn.Close()

			// Message receiver
			go func() {
				for {
					_, _, err := conn.ReadMessage()
					if err != nil {
						return
					}
					atomic.AddInt64(&messagesReceived, 1)
				}
			}()

			// Send many messages rapidly
			for j := 0; j < messagesPerClient; j++ {
				msg := ChatMessage{
					Username: fmt.Sprintf("user_%d", clientID),
					Message:  fmt.Sprintf("Rapid message %d", j),
				}

				payload, _ := json.Marshal(msg)
				header := []byte{0x00, 0x00, 0x00, 0x01}
				fullMessage := append(header, payload...)

				if err := conn.WriteMessage(websocket.BinaryMessage, fullMessage); err != nil {
					return
				}
				atomic.AddInt64(&messagesSent, 1)

				// Very small delay to allow high throughput
				if j%10 == 0 {
					time.Sleep(time.Millisecond)
				}
			}

			time.Sleep(2 * time.Second)
		}(i)

		time.Sleep(10 * time.Millisecond)
	}

	wg.Wait()
	duration := time.Since(startTime)

	messagesPerSec := float64(messagesSent) / duration.Seconds()

	log.Printf("\n=== Concurrent Messaging Stress Test Results ===")
	log.Printf("Duration: %v", duration)
	log.Printf("Clients: %d", numClients)
	log.Printf("Messages Sent: %d", messagesSent)
	log.Printf("Messages Received: %d", messagesReceived)
	log.Printf("Messages/sec: %.2f", messagesPerSec)
	log.Printf("Throughput: %.2f MB/sec", float64(messagesSent*100)/1024/1024/duration.Seconds())

	if messagesSent < int64(numClients*messagesPerClient*0.95) {
		t.Errorf("Too many failed sends: expected ~%d, got %d",
			numClients*messagesPerClient, messagesSent)
	}
}
