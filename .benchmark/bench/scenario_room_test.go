package bench

import "testing"

// TestScenarioRoom measures the echo server with every client in a
// RoomManager room (join/leave on connect/disconnect), no Ticker.
func TestScenarioRoom(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping heavy benchmark scenario in -short mode")
	}
	rows, err := RunScenario("room", true, false, DefaultLoads())
	if err != nil {
		t.Fatalf("room scenario: %v", err)
	}
	if err := WriteResults("../results", "room", rows); err != nil {
		t.Fatalf("write results: %v", err)
	}
}
