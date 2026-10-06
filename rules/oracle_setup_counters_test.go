package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// Wurmwall Sweeper's station static ("STATION 4+": an artifact creature with
// flying) is gated on four charge counters, so it is the probe that shows
// the setup counters reached the layer system, not just the object.
const setupCountersScenario = `{
 "name":"setup-counters","cr":["122.1"],"why":"setup counters and speed",
 "setup":{"p0":{"battlefield":["Wurmwall Sweeper"],"counters":{"Wurmwall Sweeper":{"CHARGE":4}},"speed":3},
          "p1":{"battlefield":["Grizzly Bears"]}},
 "steps":[],"expect":[]}`

// runSetupScenario plays raw to its end and returns the engine's final state
// alongside the result.
func runSetupScenario(t *testing.T, raw string) (OracleResult, *oracleRun) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	sc, err := decodeOracleScenario([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	sc.xmageFixture = true
	fails, transcript, run := runOracleScenario(reg, sc)
	if len(fails) != 0 {
		t.Fatalf("scenario failed: %v\n%s", fails, strings.Join(transcript, "\n"))
	}
	return OracleResult{Fails: fails, Transcript: transcript, Snapshots: run.snaps}, run
}

func setupSweeper(res OracleResult) (OracleSnapPerm, bool) {
	for _, p := range res.Snapshots[0].Permanents {
		if p.Name == "Wurmwall Sweeper" {
			return p, true
		}
	}
	return OracleSnapPerm{}, false
}

func TestOracleSetupCounters(t *testing.T) {
	res, run := runSetupScenario(t, setupCountersScenario)
	sw, ok := setupSweeper(res)
	if !ok {
		t.Fatal("Wurmwall Sweeper is not on the battlefield after setup")
	}
	if sw.Counters["CHARGE"] != 4 {
		t.Fatalf("CHARGE counters = %v, want 4", sw.Counters)
	}
	// The counters turned the station on: an artifact creature with flying.
	if sw.PT == "" || !containsStr(sw.Types, "Creature") || !containsStr(sw.Keywords, "Flying") {
		t.Fatalf("station static did not apply with 4 charge counters: %+v", sw)
	}

	// The counters came through events.Apply, not a state poke: the log holds
	// the CounterChange, and a run without the field is a plain artifact.
	var changes int
	for _, ev := range run.e.L.Events {
		if ev.Kind == events.CounterChange && ev.Counter == "CHARGE" && ev.Amount == 4 {
			changes++
		}
	}
	if changes != 1 {
		t.Fatalf("log holds %d CHARGE CounterChange events, want 1", changes)
	}
	bare := strings.Replace(setupCountersScenario, `"counters":{"Wurmwall Sweeper":{"CHARGE":4}},`, "", 1)
	if bare == setupCountersScenario {
		t.Fatal("test bug: the counters field was not removed")
	}
	bareRes, _ := runSetupScenario(t, bare)
	plain, _ := setupSweeper(bareRes)
	if plain.PT != "" || containsStr(plain.Types, "Creature") || len(plain.Counters) != 0 {
		t.Fatalf("without counters the Spacecraft must stay a plain artifact: %+v", plain)
	}

	// Replaying the same scenario reproduces the log byte for byte.
	_, again := runSetupScenario(t, setupCountersScenario)
	if run.e.L.Head() != again.e.L.Head() {
		t.Fatalf("log head differs between runs: %s vs %s", run.e.L.Head(), again.e.L.Head())
	}
}

func TestOracleSetupSpeed(t *testing.T) {
	_, run := runSetupScenario(t, setupCountersScenario)
	if got := run.e.G.Players[0].Speed; got != 3 {
		t.Fatalf("seat 0 speed = %d, want 3", got)
	}
	if got := run.e.G.Players[1].Speed; got != 0 {
		t.Fatalf("seat 1 speed = %d, want 0 (only seat 0 set it)", got)
	}
	var changes []int32
	for _, ev := range run.e.L.Events {
		if ev.Kind == events.SpeedChange && ev.Player == state.PlayerID(0) {
			changes = append(changes, ev.Amount)
		}
	}
	if len(changes) != 1 || changes[0] != 3 {
		t.Fatalf("log SpeedChange amounts = %v, want [3]", changes)
	}
}

// A counters key or speed the runner cannot place fails the scenario loudly.
func TestOracleSetupCountersRejectsTypos(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for name, raw := range map[string]string{
		"counters on a card not on the battlefield": strings.Replace(setupCountersScenario, `{"Wurmwall Sweeper":{"CHARGE":4}}`, `{"Grizzly Bears":{"CHARGE":4}}`, 1),
		"zero amount":     strings.Replace(setupCountersScenario, `"CHARGE":4`, `"CHARGE":0`, 1),
		"speed above max": strings.Replace(setupCountersScenario, `"speed":3`, `"speed":5`, 1),
		"unknown field":   strings.Replace(setupCountersScenario, `"speed":3`, `"speeed":3`, 1),
	} {
		if raw == setupCountersScenario {
			t.Fatalf("%s: test bug, the mutation changed nothing", name)
		}
		sc, err := decodeOracleScenario([]byte(raw))
		if err != nil {
			continue // strict decode rejected it
		}
		if fails, _, _ := runOracleScenario(reg, sc); len(fails) == 0 {
			t.Errorf("%s: the runner accepted it", name)
		}
	}
}
