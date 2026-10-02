package mzplay

import (
	"os"
	"strconv"
	"testing"
)

// TestTraceGame prints the searched decisions of one game: a debugging aid,
// run with MZPLAY_TRACE=<seed> (and MZPLAY_TRACE_BUDGET, MZPLAY_TRACE_LINES).
func TestTraceGame(t *testing.T) {
	seed, err := strconv.ParseUint(os.Getenv("MZPLAY_TRACE"), 10, 64)
	if err != nil {
		t.Skip("set MZPLAY_TRACE=<seed> to trace one game")
	}
	budget, _ := strconv.Atoi(os.Getenv("MZPLAY_TRACE_BUDGET"))
	if budget < 1 {
		budget = 12
	}
	lines, _ := strconv.Atoi(os.Getenv("MZPLAY_TRACE_LINES"))
	if lines < 1 {
		lines = 400
	}
	gs := testSetup(t, "mono-green-stompy", "mono-white-equipment", seed, budget)
	n := 0
	gs.MaxSubmits = 3000
	gs.Trace = func(line string) {
		if n++; n <= lines {
			t.Log(line)
		}
	}
	r, err := PlayGame(gs)
	t.Logf("turns %d winner %d stats %+v err %v", r.Turns, r.Winner, r.Stats, err)
}
