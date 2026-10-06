package rules

import (
	"reflect"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/events"
)

// Multiple kinds, duplicate placements and both seats exercise setup ordering;
// folding only the recorded events must reconstruct counters AND speed.
func TestOracleSetupCountersAndSpeedReplay(t *testing.T) {
	const raw = `{
 "name":"setup-state-replay","cr":["122.1","702.179"],
 "setup":{"p0":{"battlefield":["Wurmwall Sweeper","Wurmwall Sweeper"],
                   "counters":{"Wurmwall Sweeper":{"SHIELD":1,"CHARGE":4}},"speed":4},
          "p1":{"battlefield":["Grizzly Bears"],
                   "counters":{"Grizzly Bears":{"GROWTH":2}},"speed":2}},
 "steps":[],"expect":[]}`
	res, run := runSetupScenario(t, raw)
	if len(res.Snapshots) != 1 {
		t.Fatalf("setup snapshots = %d, want 1", len(res.Snapshots))
	}
	var sweepers, bears int
	for _, p := range res.Snapshots[0].Permanents {
		switch p.Name {
		case "Wurmwall Sweeper":
			sweepers++
			if p.Counters["CHARGE"] != 4 || p.Counters["SHIELD"] != 1 || !containsStr(p.Types, "Creature") {
				t.Fatalf("station placement did not receive both counters: %+v", p)
			}
		case "Grizzly Bears":
			bears++
			if p.Counters["GROWTH"] != 2 {
				t.Fatalf("seat 1 placement did not receive its counters: %+v", p)
			}
		}
	}
	if sweepers != 2 || bears != 1 {
		t.Fatalf("battlefield placements: sweepers=%d bears=%d, want 2/1", sweepers, bears)
	}
	if run.e.G.Players[0].Speed != 4 || run.e.G.Players[1].Speed != 2 {
		t.Fatalf("setup speeds = %d/%d, want 4/2", run.e.G.Players[0].Speed, run.e.G.Players[1].Speed)
	}
	var counters, speeds int
	for _, ev := range run.e.L.Events {
		if ev.Kind == events.CounterChange {
			counters++
		}
		if ev.Kind == events.SpeedChange {
			speeds++
		}
	}
	if counters != 5 || speeds != 2 {
		t.Fatalf("setup event counts: counters=%d speeds=%d, want 5/2", counters, speeds)
	}
	if diff := diffGames(run.e.G, replayFromLog(t, run.cfg, run.e.L.Events)); diff != "" {
		t.Fatalf("log-only setup replay differs:\n%s", diff)
	}

	// Reversing a JSON object's member order must not change the emitted log.
	reordered := strings.Replace(raw, `"SHIELD":1,"CHARGE":4`, `"CHARGE":4,"SHIELD":1`, 1)
	if reordered == raw {
		t.Fatal("precondition: JSON member order was not reversed")
	}
	_, again := runSetupScenario(t, reordered)
	if !reflect.DeepEqual(run.e.L.Events, again.e.L.Events) || run.e.L.Head() != again.e.L.Head() {
		t.Fatal("reordering setup counter kinds changed the event stream or its head")
	}
}
