package bench

import (
	"testing"
	"time"
)

// TestTickerJitter measures how far the Ticker's actual tick interval drifts
// from its configured 50ms (20 Hz) interval as connection count grows, with
// and without a RoomManager-scoped broadcast attached to the tick.
func TestTickerJitter(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping heavy benchmark scenario in -short mode")
	}

	const interval = 50 * time.Millisecond
	var rows []JitterResult
	for _, n := range DefaultLoads() {
		for _, withRoom := range []bool{false, true} {
			r, err := MeasureTickerJitter(withRoom, n, interval)
			if err != nil {
				t.Fatalf("jitter @ %d conns, room=%v: %v", n, withRoom, err)
			}
			rows = append(rows, r)
		}
	}

	if err := WriteJitterResults("../results", rows); err != nil {
		t.Fatalf("write jitter results: %v", err)
	}
}
