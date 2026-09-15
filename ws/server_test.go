package ws_test

import (
	"crypto/tls"
	"testing"

	"github.com/luciancaetano/knet/ws"
)

func TestWithTLS(t *testing.T) {
	t.Parallel()

	cfg := ws.NewConfig(":8443", ws.DefaultRateLimitConfig(), ws.AllOrigins(), nil, nil)
	cfg = ws.WithTLS(cfg, "cert.pem", "key.pem")

	if cfg.TLSCertFile != "cert.pem" {
		t.Errorf("TLSCertFile = %q, want cert.pem", cfg.TLSCertFile)
	}
	if cfg.TLSKeyFile != "key.pem" {
		t.Errorf("TLSKeyFile = %q, want key.pem", cfg.TLSKeyFile)
	}
}

func TestWithTLSConfig(t *testing.T) {
	t.Parallel()

	tlsCfg := &tls.Config{MinVersion: tls.VersionTLS13}
	cfg := ws.NewConfig(":8443", ws.DefaultRateLimitConfig(), ws.AllOrigins(), nil, nil)
	cfg = ws.WithTLSConfig(cfg, tlsCfg)

	if cfg.TLSConfig != tlsCfg {
		t.Error("TLSConfig was not set to the provided *tls.Config")
	}
}
