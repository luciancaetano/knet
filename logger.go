package knet

import "log/slog"

// Logger is the diagnostic logging interface used internally by knet.
// Implement it to route knet log output into your application's logging
// infrastructure (zerolog, zap, slog, etc.).
//
// Key-value pairs passed via args follow the same conventions as [log/slog]:
// alternating key (string) / value (any) pairs, e.g.
//
//	Warn("rate limit exceeded", "client_id", id, "addr", addr)
type Logger interface {
	Info(msg string, args ...any)
	Warn(msg string, args ...any)
	Error(msg string, args ...any)
}

// DefaultLogger returns a Logger backed by the process-wide [slog.Default]
// handler. Configure slog in your main function to control the output format
// and destination.
func DefaultLogger() Logger { return &slogLogger{slog.Default()} }

// NopLogger returns a Logger that silently discards all output.
// Useful in tests or when knet diagnostics are unwanted.
func NopLogger() Logger { return nopLogger{} }

// slogLogger adapts *slog.Logger to the Logger interface.
type slogLogger struct{ l *slog.Logger }

func (s *slogLogger) Info(msg string, args ...any)  { s.l.Info(msg, args...) }
func (s *slogLogger) Warn(msg string, args ...any)  { s.l.Warn(msg, args...) }
func (s *slogLogger) Error(msg string, args ...any) { s.l.Error(msg, args...) }

// nopLogger discards every log entry.
type nopLogger struct{}

func (nopLogger) Info(string, ...any)  {}
func (nopLogger) Warn(string, ...any)  {}
func (nopLogger) Error(string, ...any) {}
