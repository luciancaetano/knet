package knet

import "testing"

func TestNopMetrics(t *testing.T) {
	m := NopMetrics()
	m.IncCounter("c", "tag", "v")
	m.ObserveHistogram("h", 1.5, "tag", "v")
	m.SetGauge("g", 2.0)
}
