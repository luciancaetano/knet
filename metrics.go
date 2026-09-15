package knet

// Metrics is the diagnostic instrumentation interface used internally by
// knet. Implement it to route knet metrics into your application's
// observability stack (Prometheus, StatsD, OpenTelemetry, …).
//
// tags are optional alternating key/value string pairs, e.g.
//
//	IncCounter("knet_conn_rejected_total", "reason", "max_conn")
//
// Counters currently emitted by the ws server: knet_conn_accepted_total,
// knet_conn_rejected_total{reason}, knet_conn_closed_total{voluntary},
// knet_msg_received_total, knet_ratelimit_rejected_total,
// knet_handler_panic_total, knet_handler_queue_full_total,
// knet_broadcast_fail_total, knet_shutdown_forced_close_total. Gauge:
// knet_conns_active.
type Metrics interface {
	IncCounter(name string, tags ...string)
	ObserveHistogram(name string, value float64, tags ...string)
	SetGauge(name string, value float64, tags ...string)
}

// NopMetrics returns a Metrics implementation that discards everything.
// This is the default when no Metrics is configured.
func NopMetrics() Metrics { return nopMetrics{} }

type nopMetrics struct{}

func (nopMetrics) IncCounter(string, ...string)                {}
func (nopMetrics) ObserveHistogram(string, float64, ...string) {}
func (nopMetrics) SetGauge(string, float64, ...string)         {}
