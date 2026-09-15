// Command benchmark runs every knet performance scenario (baseline, room,
// ticker, room+ticker, and ticker jitter), writes CSV results under
// results/, and renders PNG charts into images/ and ../docs-site/docs/assets/benchmark/.
//
// Usage: go run . [-loads 100,500,1000]
// Or run a single scenario: go test -run TestScenarioRoom -v
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	benchmark "github.com/luciancaetano/knet/benchmark/bench"
	"github.com/luciancaetano/knet/benchmark/chart"
)

func main() {
	loadsFlag := flag.String("loads", "", "comma-separated connection counts, e.g. 100,500,1000 (overrides BENCH_LOADS)")
	flag.Parse()
	if *loadsFlag != "" {
		os.Setenv("BENCH_LOADS", *loadsFlag) //nolint:errcheck
	}

	loads := benchmark.DefaultLoads()
	log.Printf("running benchmark suite for loads=%v", loads)

	type scenario struct {
		name              string
		withRoom, withTck bool
	}
	scenarios := []scenario{
		{"baseline", false, false},
		{"room", true, false},
		{"ticker", false, true},
		{"room_ticker", true, true},
	}

	for _, s := range scenarios {
		log.Printf("scenario %s: starting", s.name)
		rows, err := benchmark.RunScenario(s.name, s.withRoom, s.withTck, loads)
		if err != nil {
			log.Fatalf("scenario %s failed: %v", s.name, err)
		}
		if err := benchmark.WriteResults("results", s.name, rows); err != nil {
			log.Fatalf("scenario %s: write results: %v", s.name, err)
		}
		log.Printf("scenario %s: done", s.name)
	}

	log.Printf("ticker jitter: starting")
	var jitterRows []benchmark.JitterResult
	for _, n := range loads {
		for _, withRoom := range []bool{false, true} {
			r, err := benchmark.MeasureTickerJitter(withRoom, n, 50*time.Millisecond)
			if err != nil {
				log.Fatalf("ticker jitter @ %d conns room=%v: %v", n, withRoom, err)
			}
			jitterRows = append(jitterRows, r)
		}
	}
	if err := benchmark.WriteJitterResults("results", jitterRows); err != nil {
		log.Fatalf("write jitter results: %v", err)
	}
	log.Printf("ticker jitter: done")

	log.Printf("generating charts")
	if err := chart.Generate("results", "images", "../docs-site/docs/assets/benchmark"); err != nil {
		log.Fatalf("chart generation failed: %v", err)
	}
	fmt.Println("benchmark suite complete: results/ (CSV), images/ (PNG)")
}
