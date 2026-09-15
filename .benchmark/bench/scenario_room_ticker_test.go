package bench

import "testing"

// TestScenarioRoomTicker measures the combined worst case: RoomManager +
// Ticker (room-scoped broadcast every tick, plus ping/RTT).
func TestScenarioRoomTicker(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping heavy benchmark scenario in -short mode")
	}
	rows, err := RunScenario("room_ticker", true, true, DefaultLoads())
	if err != nil {
		t.Fatalf("room_ticker scenario: %v", err)
	}
	if err := WriteResults("../results", "room_ticker", rows); err != nil {
		t.Fatalf("write results: %v", err)
	}
}
