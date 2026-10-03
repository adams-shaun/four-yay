package mzplay

import (
	"os"
	"reflect"
	"strconv"
	"syscall"
	"testing"
	"time"
)

// TestReuseTreeMeasure is tree reuse's cost report (azmcts.Options.ReuseTree),
// not a check: it runs only with AZ_MEASURE_REUSE=1 (at GOMAXPROCS=1, so
// process CPU time is the games' own). It plays AZ_GAMES self-play games
// (default 4) at budget AZ_SIMS (default 300) with mzplay's knobs (the
// benchmark's, the action discount 0.99, the offline GameStateEvaluator3
// leaf, the full root with macros), opponent nodes when AZ_OPP=1, each game
// once with reuse off and once on, and reports the hit rate, the misses by
// reason, the visits carried, simulations per second and per game, and how
// many games came out identical (records and all) to the game without
// reuse. AZ_REUSE_ONLY=on or off plays one side only (for a profile).
func TestReuseTreeMeasure(t *testing.T) {
	if os.Getenv("AZ_MEASURE_REUSE") != "1" {
		t.Skip("measurement only: AZ_MEASURE_REUSE=1")
	}
	atoi := func(k string, d int) int {
		if v, err := strconv.Atoi(os.Getenv(k)); err == nil {
			return v
		}
		return d
	}
	games, sims, opp := atoi("AZ_GAMES", 4), atoi("AZ_SIMS", 300), os.Getenv("AZ_OPP") == "1"
	pairs := [][2]string{{"mono-green-stompy", "mono-white-equipment"}, {"mono-red-prowess", "mono-blue-tempo"}, {"uw-tempo", "ur-delver"}}
	type side struct {
		st         GameStats
		wall, cpu  time.Duration
		turns, ok  int
		identicals int
	}
	var sides [2]side
	for g := 0; g < games; g++ {
		p := pairs[g%len(pairs)]
		var res [2]GameResult
		for on := 0; on < 2; on++ {
			if only := os.Getenv("AZ_REUSE_ONLY"); (only == "on" && on == 0) || (only == "off" && on == 1) {
				continue
			}
			gs := testSetup(t, p[0], p[1], 5000+uint64(g), sims)
			gs.Index = g
			for i := range gs.Seats {
				gs.Seats[i].OpponentNodes = opp
				gs.Seats[i].ReuseTree = on == 1
			}
			c0, w0 := cpuTime(), time.Now()
			r, err := PlayGame(gs)
			sd := &sides[on]
			sd.wall += time.Since(w0)
			sd.cpu += cpuTime() - c0
			if err != nil {
				t.Logf("game %d reuse %v: %v", g, on == 1, err)
				continue
			}
			res[on] = r
			sd.st.Add(r.Stats)
			sd.turns += r.Turns
			sd.ok++
		}
		if reflect.DeepEqual(res[0].Rows, res[1].Rows) && res[0].Turns == res[1].Turns {
			sides[1].identicals++
		}
		t.Logf("game %d %s vs %s: off %d turns %d searches %d sims; on %d turns %d searches %d sims, %d hits (%d partial) %d misses, %d visits carried",
			g, p[0], p[1], res[0].Turns, res[0].Stats.Searches, res[0].Stats.Simulations,
			res[1].Turns, res[1].Stats.Searches, res[1].Stats.Simulations, res[1].Stats.ReuseHits, res[1].Stats.ReusePartial, res[1].Stats.ReuseMisses, res[1].Stats.ReuseCarried)
	}
	for on, sd := range sides {
		st := sd.st
		t.Logf("reuse %v (opp nodes %v, budget %d): games %d turns %d searches %d simulations %d completed %d failed %d",
			on == 1, opp, sims, sd.ok, sd.turns, st.Searches, st.Simulations, st.Completed, st.SimFailures)
		t.Logf("  wall %.1fs (%.2fs/game)  cpu %.1fs  sims/s wall %.0f cpu %.0f  searches/s cpu %.2f  sims/search %.1f",
			sd.wall.Seconds(), sd.wall.Seconds()/float64(max(sd.ok, 1)), sd.cpu.Seconds(),
			float64(st.Simulations)/sd.wall.Seconds(), float64(st.Simulations)/sd.cpu.Seconds(),
			float64(st.Searches)/sd.cpu.Seconds(), float64(st.Simulations)/float64(max(st.Searches, 1)))
		if on == 1 {
			tried := st.ReuseHits + st.ReuseMisses
			noTree := st.ReuseMisses - st.ReuseMissState - st.ReuseMissCandidates
			t.Logf("  reuse: hits %d of %d searched (%.1f%%), partial %d; misses no-tree %d state %d candidates %d",
				st.ReuseHits, tried, 100*float64(st.ReuseHits)/float64(max(tried, 1)), st.ReusePartial, noTree, st.ReuseMissState, st.ReuseMissCandidates)
			t.Logf("  carried visits %d: %.1f per hit, %.1f%% of the %d visits a budget-%d search would run per searched decision",
				st.ReuseCarried, float64(st.ReuseCarried)/float64(max(st.ReuseHits, 1)), 100*float64(st.ReuseCarried)/float64(max(tried*sims, 1)), tried*sims, sims)
			t.Logf("  games identical to the game without reuse: %d of %d", sd.identicals, games)
		}
	}
}

// cpuTime is the process's user+system CPU time.
func cpuTime() time.Duration {
	var ru syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &ru); err != nil {
		return 0
	}
	return time.Duration(ru.Utime.Nano() + ru.Stime.Nano())
}
