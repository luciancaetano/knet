// Example: minimal plain (non-TLS) echo server, purpose-built as the
// container image used by the Docker stress/e2e tests (tests/stress,
// tests/docker-e2e.sh). Not meant as a production example.
//
// Run:
//
//	go run . -addr :8080
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/luciancaetano/knet"
	"github.com/luciancaetano/knet/ws"
)

const EchoCommand uint32 = 0x0001

func main() {
	addr := os.Getenv("ADDR")
	if addr == "" {
		addr = ":8080"
	}

	cfg := ws.NewConfig(addr, ws.DefaultRateLimitConfig(), ws.AllOrigins(), nil, nil)
	server := ws.New(cfg)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := server.RegisterHandler(ctx, EchoCommand, func(client knet.Client, payload []byte) {
		if err := client.Send(ctx, EchoCommand, payload); err != nil {
			log.Printf("echo failed: %v", err)
		}
	}); err != nil {
		log.Fatalf("register handler: %v", err)
	}

	log.Printf("stress-echo server on %s", addr)
	if err := server.Start(ctx); err != nil {
		log.Fatalf("start: %v", err)
	}

	<-ctx.Done()
	log.Println("shutting down...")
}
