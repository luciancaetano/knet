// Package chart renders the benchmark CSV results as PNG line charts using
// gonum/plot (pure Go, no external plotting process required).
package chart

import (
	"encoding/csv"
	"fmt"
	"image/color"
	"os"
	"path/filepath"
	"sort"
	"strconv"

	"gonum.org/v1/plot"
	"gonum.org/v1/plot/plotter"
	"gonum.org/v1/plot/plotutil"
	"gonum.org/v1/plot/vg"
	"gonum.org/v1/plot/vg/draw"
	"gonum.org/v1/plot/vg/vgimg"
)

var scenarios = []string{"baseline", "room", "ticker", "room_ticker"}

// palette gives every scenario a fixed, high-contrast color that stays the
// same across latency/memory/throughput charts (index == scenarios index).
var palette = plotutil.DarkColors

// pngDPI raises output resolution above gonum's 96 default so the charts
// stay crisp at the 700px width docs-site renders them at.
const pngDPI = 150

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
			if err := savePNG(plt, 9*vg.Inch, 5*vg.Inch, filepath.Join(outDir, name)); err != nil {
				return fmt.Errorf("save %s: %w", name, err)
			}
		}
	}
	return nil
}

// savePNG renders at pngDPI instead of gonum's 96 default, since Plot.Save
// has no DPI knob.
func savePNG(p *plot.Plot, w, h vg.Length, file string) error {
	c := vgimg.NewWith(vgimg.UseWH(w, h), vgimg.UseDPI(pngDPI))
	p.Draw(draw.New(c))
	f, err := os.Create(file)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = (vgimg.PngCanvas{Canvas: c}).WriteTo(f)
	return err
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

// connTicks is a fixed tick marker for the connection-count axis: plain
// integer labels (100, 500, 1000, ...) instead of scientific notation.
type connTicks struct{ values []float64 }

func (t connTicks) Ticks(min, max float64) []plot.Tick {
	ticks := make([]plot.Tick, 0, len(t.values))
	for _, v := range t.values {
		if v < min || v > max {
			continue
		}
		ticks = append(ticks, plot.Tick{Value: v, Label: strconv.FormatFloat(v, 'f', 0, 64)})
	}
	return ticks
}

// logAxis sets up the shared log-X grid/ticks. legendTop/legendLeft pick the
// legend corner emptiest of data for that particular chart's shape — a
// single fixed corner collides with a curve on at least one of the charts.
func logAxis(p *plot.Plot, loads []float64, legendTop, legendLeft bool) {
	p.X.Scale = plot.LogScale{}
	p.X.Tick.Marker = connTicks{values: loads}
	grid := plotter.NewGrid()
	grid.Vertical.Color = color.Gray{Y: 220}
	grid.Horizontal.Color = color.Gray{Y: 220}
	p.Add(grid)
	p.Legend.Top = legendTop
	p.Legend.Left = legendLeft
}

// distinctConns collects the sorted, unique connection counts present across
// every scenario's rows, used as the fixed X-axis tick values.
func distinctConns(data map[string][]row) []float64 {
	seen := map[float64]bool{}
	var out []float64
	for _, rows := range data {
		for _, r := range rows {
			if r.connections > 0 && !seen[r.connections] {
				seen[r.connections] = true
				out = append(out, r.connections)
			}
		}
	}
	sort.Float64s(out)
	return out
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

// addSeries draws one line+markers series in the palette color at index idx,
// used instead of plotutil.AddLinePoints so every chart shares the same
// color/shape per scenario with a thicker, more legible line.
func addSeries(p *plot.Plot, name string, pts plotter.XYs, idx int) error {
	if len(pts) == 0 {
		return nil
	}
	line, points, err := plotter.NewLinePoints(pts)
	if err != nil {
		return err
	}
	c := palette[idx%len(palette)]
	line.Color = c
	line.Width = vg.Points(2)
	points.Color = c
	points.Shape = plotutil.Shape(idx)
	points.Radius = vg.Points(3)
	p.Add(line, points)
	p.Legend.Add(name, line, points)
	return nil
}

// labelPoints adds a text label only on the last point of the series (the
// value most readers care about, and the only spot guaranteed not to collide
// with a neighboring series at the same X).
func labelPoints(p *plot.Plot, pts plotter.XYs, format string) error {
	if len(pts) == 0 {
		return nil
	}
	last := pts[len(pts)-1]
	l, err := plotter.NewLabels(plotter.XYLabels{
		XYs:    plotter.XYs{last},
		Labels: []string{fmt.Sprintf(format, last.Y)},
	})
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
	logAxis(p, distinctConns(data), true, true)

	for i, s := range scenarios {
		pts := toXY(data[s], func(r row) float64 { return r.p99 })
		if err := addSeries(p, s, pts, i); err != nil {
			return nil, err
		}
		if err := labelPoints(p, pts, "%.2f"); err != nil {
			return nil, err
		}
	}
	return p, nil
}

func memoryPlot(data map[string][]row) (*plot.Plot, error) {
	p := plot.New()
	p.Title.Text = "Memory per Connection vs Connections"
	p.X.Label.Text = "connections"
	p.Y.Label.Text = "bytes / connection"
	p.Y.Min = 0
	logAxis(p, distinctConns(data), false, false)

	for i, s := range scenarios {
		pts := toXY(data[s], func(r row) float64 { return r.bytesPerConn })
		if err := addSeries(p, s, pts, i); err != nil {
			return nil, err
		}
		if err := labelPoints(p, pts, "%.0f"); err != nil {
			return nil, err
		}
	}
	return p, nil
}

func throughputPlot(data map[string][]row) (*plot.Plot, error) {
	p := plot.New()
	p.Title.Text = "Throughput vs Connections"
	p.X.Label.Text = "connections"
	p.Y.Label.Text = "messages / sec"
	logAxis(p, distinctConns(data), true, true)

	for i, s := range scenarios {
		pts := toXY(data[s], func(r row) float64 { return r.throughput })
		if err := addSeries(p, s, pts, i); err != nil {
			return nil, err
		}
		if err := labelPoints(p, pts, "%.0f"); err != nil {
			return nil, err
		}
	}
	return p, nil
}

func jitterPlot(rows []jitterRow) (*plot.Plot, error) {
	p := plot.New()
	p.Title.Text = "Ticker Jitter (p99) vs Connections"
	p.X.Label.Text = "connections"
	p.Y.Label.Text = "tick interval drift (ms)"

	seen := map[float64]bool{}
	var loads []float64
	byMode := map[string]plotter.XYs{}
	for _, r := range rows {
		if r.connections <= 0 {
			continue
		}
		if !seen[r.connections] {
			seen[r.connections] = true
			loads = append(loads, r.connections)
		}
		byMode[r.mode] = append(byMode[r.mode], struct{ X, Y float64 }{X: r.connections, Y: r.p99})
	}
	sort.Float64s(loads)
	logAxis(p, loads, true, true)

	for i, mode := range []string{"no_room", "room"} {
		pts := byMode[mode]
		if err := addSeries(p, mode+" p99 drift", pts, i); err != nil {
			return nil, err
		}
		if err := labelPoints(p, pts, "%.2f"); err != nil {
			return nil, err
		}
	}
	return p, nil
}
