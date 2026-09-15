package bench

import "testing"

// TestScenarioTicker measures the echo server with a 20 Hz Ticker
// (TimeManager, global broadcast + ping/RTT) running, no RoomManager.
func TestScenarioTicker(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping heavy benchmark scenario in -short mode")
	}
	rows, err := RunScenario("ticker", false, true, DefaultLoads())
	if err != nil {
		t.Fatalf("ticker scenario: %v", err)
	}
	if err := WriteResults("../results", "ticker", rows); err != nil {
		t.Fatalf("write results: %v", err)
	}
}
