package knet

import (
	"context"
)

// fakeServer is a minimal Server implementation for testing HandleJSONRPC.
type fakeServer struct {
	broadcasts []struct {
		cmd     uint32
		payload []byte
	}
	broadcastErr error
	jsonrpc      map[string]JSONRPCHandler
	handlers     map[uint32]HandlerFunc
	registerErr  error
}

func (s *fakeServer) Start(ctx context.Context) error { return nil }
func (s *fakeServer) Stop(ctx context.Context) error  { return nil }
func (s *fakeServer) RegisterHandler(ctx context.Context, commandID uint32, handler HandlerFunc) error {
	if s.registerErr != nil {
		return s.registerErr
	}
	if s.handlers == nil {
		s.handlers = make(map[uint32]HandlerFunc)
	}
	s.handlers[commandID] = handler
	return nil
}
func (s *fakeServer) RegisterJSONRPCHandler(ctx context.Context, method string, handler JSONRPCHandler) error {
	if s.registerErr != nil {
		return s.registerErr
	}
	if s.jsonrpc == nil {
		s.jsonrpc = make(map[string]JSONRPCHandler)
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

// fakeClient is a minimal Client implementation for testing ConnectHooks.
type fakeClient struct {
	id string
}

func (c *fakeClient) ID() string                           { return c.id }
func (c *fakeClient) RemoteAddr() string                   { return "127.0.0.1:0" }
func (c *fakeClient) Context() context.Context             { return context.Background() }
func (c *fakeClient) IsAlive() bool                        { return true }
func (c *fakeClient) ConnectionPayload() ConnectionPayload { return nil }
func (c *fakeClient) Close(ctx context.Context) error      { return nil }
func (c *fakeClient) CloseWithCode(ctx context.Context, code int, reason string) error {
	return nil
}
func (c *fakeClient) Send(ctx context.Context, command uint32, payload []byte) error { return nil }
