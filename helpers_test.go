package knet

import (
	"context"
	"errors"
	"testing"
)

type moveReq struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type moveResp struct {
	OK bool `json:"ok"`
}

func TestHandleJSONRPCSuccess(t *testing.T) {
	srv := &fakeServer{}
	err := HandleJSONRPC(context.Background(), srv, "player.move", func(req moveReq) (moveResp, error) {
		return moveResp{OK: req.X == 1 && req.Y == 2}, nil
	})
	if err != nil {
		t.Fatalf("HandleJSONRPC register error = %v", err)
	}

	h, ok := srv.jsonrpc["player.move"]
	if !ok {
		t.Fatal("handler not registered")
	}

	res, err := h(map[string]interface{}{"x": 1.0, "y": 2.0})
	if err != nil {
		t.Fatalf("handler() error = %v", err)
	}
	resp, ok := res.(moveResp)
	if !ok || !resp.OK {
		t.Fatalf("unexpected response: %#v", res)
	}
}

func TestHandleJSONRPCUnmarshalError(t *testing.T) {
	srv := &fakeServer{}
	_ = HandleJSONRPC(context.Background(), srv, "bad", func(req moveReq) (moveResp, error) {
		return moveResp{}, nil
	})
	h := srv.jsonrpc["bad"]

	// x expects a number, giving a string should fail json.Unmarshal into moveReq.
	_, err := h(map[string]interface{}{"x": "not-a-number"})
	if err == nil {
		t.Fatal("expected unmarshal error")
	}
}

func TestHandleJSONRPCFnError(t *testing.T) {
	srv := &fakeServer{}
	wantErr := errors.New("boom")
	_ = HandleJSONRPC(context.Background(), srv, "fails", func(req moveReq) (moveResp, error) {
		return moveResp{}, wantErr
	})
	h := srv.jsonrpc["fails"]

	_, err := h(map[string]interface{}{"x": 1.0, "y": 2.0})
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected wantErr, got %v", err)
	}
}

func TestHandleJSONRPCRegisterError(t *testing.T) {
	registerErr := errors.New("register failed")
	srv := &fakeServer{registerErr: registerErr}
	err := HandleJSONRPC(context.Background(), srv, "x", func(req moveReq) (moveResp, error) {
		return moveResp{}, nil
	})
	if !errors.Is(err, registerErr) {
		t.Fatalf("expected registerErr, got %v", err)
	}
}
