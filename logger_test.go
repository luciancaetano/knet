package knet

import "testing"

func TestDefaultLogger(t *testing.T) {
	l := DefaultLogger()
	// Just exercise the code paths; slog.Default() writes to stderr, we don't
	// assert on output, only that it doesn't panic.
	l.Info("info msg", "k", "v")
	l.Warn("warn msg", "k", "v")
	l.Error("error msg", "k", "v")
}

func TestNopLogger(t *testing.T) {
	l := NopLogger()
	l.Info("x")
	l.Warn("x")
	l.Error("x")
}
