package bench

import (
	"encoding/csv"
	"os"
	"path/filepath"
	"strconv"
)

var resultsHeader = []string{
	"scenario", "connections", "p50_ms", "p95_ms", "p99_ms",
	"throughput_msg_s", "bytes_per_conn", "ticks_measured", "jitter_ms",
}

// writeResults overwrites resultsDir/<scenario>.csv with one row per Result.
func WriteResults(resultsDir, scenario string, rows []Result) error {
	if err := os.MkdirAll(resultsDir, 0o755); err != nil {
		return err
	}
	f, err := os.Create(filepath.Join(resultsDir, scenario+".csv"))
	if err != nil {
		return err
	}
	defer f.Close()

	w := csv.NewWriter(f)
	defer w.Flush()

	if err := w.Write(resultsHeader); err != nil {
		return err
	}
	for _, r := range rows {
		record := []string{
			r.Scenario,
			strconv.Itoa(r.Connections),
			strconv.FormatFloat(r.P50Ms, 'f', 3, 64),
			strconv.FormatFloat(r.P95Ms, 'f', 3, 64),
			strconv.FormatFloat(r.P99Ms, 'f', 3, 64),
			strconv.FormatFloat(r.ThroughputMsgS, 'f', 2, 64),
			strconv.FormatFloat(r.BytesPerConn, 'f', 2, 64),
			strconv.FormatUint(r.TicksMeasured, 10),
			strconv.FormatFloat(r.JitterMs, 'f', 4, 64),
		}
		if err := w.Write(record); err != nil {
			return err
		}
	}
	return w.Error()
}

var jitterHeader = []string{
	"connections", "mode", "expected_interval_ms", "mean_drift_ms", "p99_drift_ms", "max_drift_ms",
}

// writeJitterResults overwrites resultsDir/ticker_jitter.csv.
func WriteJitterResults(resultsDir string, rows []JitterResult) error {
	if err := os.MkdirAll(resultsDir, 0o755); err != nil {
		return err
	}
	f, err := os.Create(filepath.Join(resultsDir, "ticker_jitter.csv"))
	if err != nil {
		return err
	}
	defer f.Close()

	w := csv.NewWriter(f)
	defer w.Flush()

	if err := w.Write(jitterHeader); err != nil {
		return err
	}
	for _, r := range rows {
		record := []string{
			strconv.Itoa(r.Connections),
			r.Mode,
			strconv.FormatFloat(r.ExpectedIntervalMs, 'f', 3, 64),
			strconv.FormatFloat(r.MeanDriftMs, 'f', 4, 64),
			strconv.FormatFloat(r.P99DriftMs, 'f', 4, 64),
			strconv.FormatFloat(r.MaxDriftMs, 'f', 4, 64),
		}
		if err := w.Write(record); err != nil {
			return err
		}
	}
	return w.Error()
}
