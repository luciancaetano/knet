// Package chart renders the benchmark CSV results as PNG line charts using
// gonum/plot (pure Go, no external plotting process required).
package chart

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"gonum.org/v1/plot"
	"gonum.org/v1/plot/plotter"
	"gonum.org/v1/plot/plotutil"
	"gonum.org/v1/plot/vg"
)

var scenarios = []string{"baseline", "room", "ticker", "room_ticker"}

type row struct {
	connections   float64
	p50, p95, p99 float64
	throughput    float64
	bytesPerConn  float64
}

// Generate reads every scenario CSV from resultsDir and the ticker_jitter.csv
// alongside it, and writes latency.png, memory_per_conn.png, throughput.png
// and ticker_jitter.png into each of outDirs.
func Generate(resultsDir string, outDirs ...string) error {
	data := map[string][]row{}
	for _, s := range scenarios {
		rows, err := readScenarioCSV(filepath.Join(resultsDir, s+".csv"))
		if err != nil {
			return fmt.Errorf("read %s: %w", s, err)
		}
		data[s] = rows
	}

	jitter, err := readJitterCSV(filepath.Join(resultsDir, "ticker_jitter.csv"))
	if err != nil {
		return fmt.Errorf("read ticker_jitter: %w", err)
	}

	plots := map[string]*plot.Plot{}

	p, err := latencyPlot(data)
	if err != nil {
		return err
	}
	plots["latency.png"] = p

	p, err = memoryPlot(data)
	if err != nil {
		return err
	}
	plots["memory_per_conn.png"] = p

	p, err = throughputPlot(data)
	if err != nil {
		return err
	}
	plots["throughput.png"] = p

	p, err = jitterPlot(jitter)
	if err != nil {
		return err
	}
	plots["ticker_jitter.png"] = p

	for _, outDir := range outDirs {
		if err := os.MkdirAll(outDir, 0o755); err != nil {
			return err
		}
		for name, plt := range plots {
			if err := plt.Save(9*vg.Inch, 5*vg.Inch, filepath.Join(outDir, name)); err != nil {
				return fmt.Errorf("save %s: %w", name, err)
			}
		}
	}
	return nil
}

func readScenarioCSV(path string) ([]row, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	records, err := csv.NewReader(f).ReadAll()
	if err != nil {
		return nil, err
	}
	if len(records) < 2 {
		return nil, nil
	}

	rows := make([]row, 0, len(records)-1)
	for _, rec := range records[1:] {
		// scenario,connections,p50_ms,p95_ms,p99_ms,throughput_msg_s,bytes_per_conn,ticks_measured,jitter_ms
		conns, _ := strconv.ParseFloat(rec[1], 64)
		p50, _ := strconv.ParseFloat(rec[2], 64)
		p95, _ := strconv.ParseFloat(rec[3], 64)
		p99, _ := strconv.ParseFloat(rec[4], 64)
		thr, _ := strconv.ParseFloat(rec[5], 64)
		bpc, _ := strconv.ParseFloat(rec[6], 64)
		rows = append(rows, row{connections: conns, p50: p50, p95: p95, p99: p99, throughput: thr, bytesPerConn: bpc})
	}
	return rows, nil
}

type jitterRow struct {
	connections float64
	mode        string
	mean, p99   float64
}

func readJitterCSV(path string) ([]jitterRow, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	records, err := csv.NewReader(f).ReadAll()
	if err != nil {
		return nil, err
	}
	if len(records) < 2 {
		return nil, nil
	}

	rows := make([]jitterRow, 0, len(records)-1)
	for _, rec := range records[1:] {
		// connections,mode,expected_interval_ms,mean_drift_ms,p99_drift_ms,max_drift_ms
		conns, _ := strconv.ParseFloat(rec[0], 64)
		mean, _ := strconv.ParseFloat(rec[3], 64)
		p99, _ := strconv.ParseFloat(rec[4], 64)
		rows = append(rows, jitterRow{connections: conns, mode: rec[1], mean: mean, p99: p99})
	}
	return rows, nil
}

func logAxis(p *plot.Plot) {
	p.X.Scale = plot.LogScale{}
	p.X.Tick.Marker = plot.LogTicks{}
	p.Add(plotter.NewGrid())
}

func toXY(rows []row, f func(row) float64) plotter.XYs {
	pts := make(plotter.XYs, 0, len(rows))
	for _, r := range rows {
		if r.connections <= 0 {
			continue
		}
		pts = append(pts, struct{ X, Y float64 }{X: r.connections, Y: f(r)})
	}
	return pts
}

// labelPoints adds a text label with each point's Y value above the marker,
// so exact values are readable without hovering/zooming.
func labelPoints(p *plot.Plot, pts plotter.XYs, format string) error {
	labels := make([]string, len(pts))
	for i, pt := range pts {
		labels[i] = fmt.Sprintf(format, pt.Y)
	}
	l, err := plotter.NewLabels(plotter.XYLabels{XYs: pts, Labels: labels})
	if err != nil {
		return err
	}
	p.Add(l)
	return nil
}

func latencyPlot(data map[string][]row) (*plot.Plot, error) {
	p := plot.New()
	p.Title.Text = "Latency (p99) vs Connections"
	p.X.Label.Text = "connections"
	p.Y.Label.Text = "latency (ms)"
	logAxis(p)

	var args []interface{}
	for _, s := range scenarios {
		pts := toXY(data[s], func(r row) float64 { return r.p99 })
		args = append(args, s, pts)
		if err := labelPoints(p, pts, "%.2f"); err != nil {
			return nil, err
		}
	}
	if err := plotutil.AddLinePoints(p, args...); err != nil {
		return nil, err
	}
	return p, nil
}

func memoryPlot(data map[string][]row) (*plot.Plot, error) {
	p := plot.New()
	p.Title.Text = "Memory per Connection vs Connections"
	p.X.Label.Text = "connections"
	p.Y.Label.Text = "bytes / connection"
	logAxis(p)

	var args []interface{}
	for _, s := range scenarios {
		pts := toXY(data[s], func(r row) float64 { return r.bytesPerConn })
		args = append(args, s, pts)
		if err := labelPoints(p, pts, "%.0f"); err != nil {
			return nil, err
		}
	}
	if err := plotutil.AddLinePoints(p, args...); err != nil {
		return nil, err
	}
	return p, nil
}

func throughputPlot(data map[string][]row) (*plot.Plot, error) {
	p := plot.New()
	p.Title.Text = "Throughput vs Connections"
	p.X.Label.Text = "connections"
	p.Y.Label.Text = "messages / sec"
	logAxis(p)

	var args []interface{}
	for _, s := range scenarios {
		pts := toXY(data[s], func(r row) float64 { return r.throughput })
		args = append(args, s, pts)
		if err := labelPoints(p, pts, "%.0f"); err != nil {
			return nil, err
		}
	}
	if err := plotutil.AddLinePoints(p, args...); err != nil {
		return nil, err
	}
	return p, nil
}

func jitterPlot(rows []jitterRow) (*plot.Plot, error) {
	p := plot.New()
	p.Title.Text = "Ticker Jitter (p99) vs Connections"
	p.X.Label.Text = "connections"
	p.Y.Label.Text = "tick interval drift (ms)"
	logAxis(p)

	byMode := map[string]plotter.XYs{}
	for _, r := range rows {
		if r.connections <= 0 {
			continue
		}
		byMode[r.mode] = append(byMode[r.mode], struct{ X, Y float64 }{X: r.connections, Y: r.p99})
	}

	var args []interface{}
	for _, mode := range []string{"no_room", "room"} {
		pts := byMode[mode]
		args = append(args, mode+" p99 drift", pts)
		if err := labelPoints(p, pts, "%.2f"); err != nil {
			return nil, err
		}
	}
	if err := plotutil.AddLinePoints(p, args...); err != nil {
		return nil, err
	}
	return p, nil
}
