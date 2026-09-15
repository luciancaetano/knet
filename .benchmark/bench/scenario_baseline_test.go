package bench

import "testing"

// TestScenarioBaseline measures a plain echo server: no RoomManager, no
// Ticker. This is the control other scenarios are compared against.
func TestScenarioBaseline(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping heavy benchmark scenario in -short mode")
	}
	rows, err := RunScenario("baseline", false, false, DefaultLoads())
	if err != nil {
		t.Fatalf("baseline scenario: %v", err)
	}
	if err := WriteResults("../results", "baseline", rows); err != nil {
		t.Fatalf("write results: %v", err)
	}
}
