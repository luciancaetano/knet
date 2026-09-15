package timing

import (
	"context"

	"github.com/luciancaetano/knet"
)

// fakeClient is a minimal knet.Client implementation for testing TimeManager/ping.
type fakeClient struct {
	id      string
	sendErr error
	sent    []struct {
		cmd     uint32
		payload []byte
	}
	closed bool
}

func (c *fakeClient) ID() string               { return c.id }
func (c *fakeClient) RemoteAddr() string       { return "127.0.0.1:0" }
func (c *fakeClient) Context() context.Context { return context.Background() }
func (c *fakeClient) IsAlive() bool            { return !c.closed }
func (c *fakeClient) Close(ctx context.Context) error {
	c.closed = true
	return nil
}
func (c *fakeClient) CloseWithCode(ctx context.Context, code int, reason string) error {
	c.closed = true
	return nil
}
func (c *fakeClient) Send(ctx context.Context, command uint32, payload []byte) error {
	if c.sendErr != nil {
		return c.sendErr
	}
	c.sent = append(c.sent, struct {
		cmd     uint32
		payload []byte
	}{command, payload})
	return nil
}

var _ knet.Client = (*fakeClient)(nil)

// fakeServer is a minimal knet.Server implementation for testing TimeManager.
type fakeServer struct {
	broadcasts []struct {
		cmd     uint32
		payload []byte
	}
	broadcastErr error
	jsonrpc      map[string]knet.JSONRPCHandler
	handlers     map[uint32]knet.HandlerFunc
	registerErr  error
}

func (s *fakeServer) Start(ctx context.Context) error { return nil }
func (s *fakeServer) Stop(ctx context.Context) error  { return nil }
func (s *fakeServer) RegisterHandler(ctx context.Context, commandID uint32, handler knet.HandlerFunc) error {
	if s.registerErr != nil {
		return s.registerErr
	}
	if s.handlers == nil {
		s.handlers = make(map[uint32]knet.HandlerFunc)
	}
	s.handlers[commandID] = handler
	return nil
}
func (s *fakeServer) RegisterJSONRPCHandler(ctx context.Context, method string, handler knet.JSONRPCHandler) error {
	if s.registerErr != nil {
		return s.registerErr
	}
	if s.jsonrpc == nil {
		s.jsonrpc = make(map[string]knet.JSONRPCHandler)
	}
	s.jsonrpc[method] = handler
	return nil
}
func (s *fakeServer) BroadcastCommand(ctx context.Context, commandID uint32, payload []byte) error {
	if s.broadcastErr != nil {
		return s.broadcastErr
	}
	s.broadcasts = append(s.broadcasts, struct {
		cmd     uint32
		payload []byte
	}{commandID, payload})
	return nil
}

var _ knet.Server = (*fakeServer)(nil)
