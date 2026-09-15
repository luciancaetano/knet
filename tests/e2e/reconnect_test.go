package e2e_test

import (
	"context"
	"net/url"
	"testing"
	"time"

	"github.com/luciancaetano/knet"
	"github.com/luciancaetano/knet/ws"
)

// TestReconnectResume verifies that a client which reconnects with a
// previously-issued session ID resumes with the room list the application
// stored for that session (via OnResume), instead of starting fresh.
func TestReconnectResume(t *testing.T) {
	t.Parallel()

	store := newTestSessionStore()
	resumeCh := make(chan []string, 1)

	cfg := ws.NewConfig(":18082", ws.DefaultRateLimitConfig(), ws.AllOrigins(),
		func(client knet.Client) bool { return true },
		nil,
	)
	cfg.SessionStore = store
	cfg.SessionGraceTTL = time.Second
	cfg.OnResume = func(client knet.Client, previousRooms []string) bool {
		resumeCh <- previousRooms
		return true
	}
	server := ws.New(cfg)
	ctx := context.Background()

	if err := server.Start(ctx); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		server.Stop(stopCtx)
	}()

	time.Sleep(200 * time.Millisecond)

	// Simulate the application having previously stored this session's room
	// membership (normally done when the client joined rooms).
	const wantSessionID = "test-session-1"
	store.Put(wantSessionID, []string{"room-a", "room-b"}, 30*time.Second)

	u := url.URL{Scheme: "ws", Host: "localhost:18082", Path: "/ws", RawQuery: "session=" + wantSessionID}
	conn, _, err := newDialer().Dial(u.String(), nil)
	if err != nil {
		t.Fatalf("failed to reconnect with session: %v", err)
	}
	defer conn.Close()

	select {
	case rooms := <-resumeCh:
		if len(rooms) != 2 || rooms[0] != "room-a" || rooms[1] != "room-b" {
			t.Errorf("got rooms %v, want [room-a room-b]", rooms)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("OnResume was not called")
	}
}

// testSessionStore is a trivial knet.SessionStore for seeding test data.
type testSessionStore struct {
	rooms map[string][]string
}

func newTestSessionStore() *testSessionStore {
	return &testSessionStore{rooms: make(map[string][]string)}
}

func (s *testSessionStore) Get(sessionID string) ([]string, bool) {
	rooms, ok := s.rooms[sessionID]
	return rooms, ok
}

func (s *testSessionStore) Put(sessionID string, rooms []string, ttl time.Duration) {
	s.rooms[sessionID] = rooms
}

func (s *testSessionStore) Delete(sessionID string) {
	delete(s.rooms, sessionID)
}
