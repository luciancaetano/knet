// Example: routing knet metrics into Prometheus.
//
// Run:
//
//	cd examples/metrics-prometheus
//	go mod init metrics-prometheus-example && go mod tidy
//	go run .
//
// Then curl http://localhost:9090/metrics.
package main

import (
	"context"
	"log"
	"net/http"

	"github.com/luciancaetano/knet"
	"github.com/luciancaetano/knet/ws"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// promMetrics implements knet.Metrics on top of Prometheus client_golang.
// One vec per metric name is created lazily on first use since knet.Metrics
// doesn't know label names up front — we use "tags" as a single joined label.
type promMetrics struct {
	counters   map[string]*prometheus.CounterVec
	histograms map[string]*prometheus.HistogramVec
	gauges     map[string]*prometheus.GaugeVec
}

func newPromMetrics() *promMetrics {
	return &promMetrics{
		counters:   map[string]*prometheus.CounterVec{},
		histograms: map[string]*prometheus.HistogramVec{},
		gauges:     map[string]*prometheus.GaugeVec{},
	}
}

func labelPairs(tags []string) ([]string, prometheus.Labels) {
	names := make([]string, 0, len(tags)/2)
	values := prometheus.Labels{}
	for i := 0; i+1 < len(tags); i += 2 {
		names = append(names, tags[i])
		values[tags[i]] = tags[i+1]
	}
	return names, values
}

func (m *promMetrics) IncCounter(name string, tags ...string) {
	names, values := labelPairs(tags)
	c, ok := m.counters[name]
	if !ok {
		c = promauto.NewCounterVec(prometheus.CounterOpts{Name: name}, names)
		m.counters[name] = c
	}
	c.With(values).Inc()
}

func (m *promMetrics) ObserveHistogram(name string, value float64, tags ...string) {
	names, values := labelPairs(tags)
	h, ok := m.histograms[name]
	if !ok {
		h = promauto.NewHistogramVec(prometheus.HistogramOpts{Name: name}, names)
		m.histograms[name] = h
	}
	h.With(values).Observe(value)
}

func (m *promMetrics) SetGauge(name string, value float64, tags ...string) {
	names, values := labelPairs(tags)
	g, ok := m.gauges[name]
	if !ok {
		g = promauto.NewGaugeVec(prometheus.GaugeOpts{Name: name}, names)
		m.gauges[name] = g
	}
	g.With(values).Set(value)
}

func main() {
	var _ knet.Metrics = (*promMetrics)(nil)

	cfg := ws.NewConfig(":8080", ws.DefaultRateLimitConfig(), ws.AllOrigins(), nil, nil)
	cfg = ws.WithMetrics(cfg, newPromMetrics())
	server := ws.New(cfg)

	go func() {
		http.Handle("/metrics", promhttp.Handler())
		log.Println("metrics on :9090/metrics")
		log.Fatal(http.ListenAndServe(":9090", nil))
	}()

	log.Println("knet server on :8080")
	log.Fatal(server.Start(context.Background()))
}
