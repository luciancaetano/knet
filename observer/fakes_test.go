package observer

import (
	"context"
	"errors"

	"github.com/luciancaetano/knet"
)

// fakeClient is a minimal knet.Client implementation for testing ObserverSet.
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

var errFakeSend = errors.New("fake send failure")
