package bench

import "sync/atomic"

// collectorMetrics implements knet.Metrics, counting the counters the
// benchmark cares about instead of scraping logs or wrapping every Send call
// site by hand.
type collectorMetrics struct {
	msgReceived   atomic.Int64
	connsAccepted atomic.Int64
	connsActive   atomic.Int64
}

func newCollectorMetrics() *collectorMetrics { return &collectorMetrics{} }

func (c *collectorMetrics) IncCounter(name string, _ ...string) {
	switch name {
	case "knet_msg_received_total":
		c.msgReceived.Add(1)
	case "knet_conn_accepted_total":
		c.connsAccepted.Add(1)
	}
}

func (c *collectorMetrics) ObserveHistogram(string, float64, ...string) {}

func (c *collectorMetrics) SetGauge(name string, value float64, _ ...string) {
	if name == "knet_conns_active" {
		c.connsActive.Store(int64(value))
	}
}

func (c *collectorMetrics) MessagesReceived() int64  { return c.msgReceived.Load() }
func (c *collectorMetrics) ActiveConnections() int64 { return c.connsActive.Load() }
