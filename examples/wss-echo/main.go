// Example: a minimal echo server over wss:// (TLS).
//
// Generate a self-signed dev certificate first:
//
//	openssl req -x509 -newkey rsa:2048 -nodes -keyout key.pem -out cert.pem \
//	    -days 365 -subj "/CN=localhost"
//
// Run:
//
//	go run . -cert cert.pem -key key.pem
//
// Test with wscat (accepts the untrusted self-signed cert):
//
//	wscat -c wss://localhost:8443/ws --no-check
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/luciancaetano/knet"
	"github.com/luciancaetano/knet/ws"
)

const EchoCommand uint32 = 0x0001

func main() {
	addr := flag.String("addr", ":8443", "listen address")
	certFile := flag.String("cert", "cert.pem", "TLS certificate file")
	keyFile := flag.String("key", "key.pem", "TLS key file")
	flag.Parse()

	cfg := ws.NewConfig(*addr, ws.DefaultRateLimitConfig(), ws.AllOrigins(), nil, nil)
	cfg = ws.WithTLS(cfg, *certFile, *keyFile)
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

	log.Printf("wss echo server on %s", *addr)
	if err := server.Start(ctx); err != nil {
		log.Fatalf("start: %v", err)
	}

	<-ctx.Done()
	log.Println("shutting down...")
}
