package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// runFixtureScenario plays a scenario as RunOracleScenarioJSON does
// (xmageFixture) and fails the test on any scenario failure.
func runFixtureScenario(t *testing.T, raw string) (OracleResult, *oracleRun) {
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

func setupPermCount(snap OracleSnapshot, name string) int {
	n := 0
	for _, p := range snap.Permanents {
		if p.Name == name {
			n++
		}
	}
	return n
}

// XMage's addCard(Zone.BATTLEFIELD) never puts an enters-the-battlefield
// trigger on the stack, so a setup permanent's ETB must not draw, gain life,
// make tokens or search at the setup checkpoint.
func TestOracleSetupPlacementFiresNoETB(t *testing.T) {
	// Greed's Gambit: ETB draw three and gain 6 life.
	res, _ := runFixtureScenario(t, `{"name":"gambit","setup":{"p0":{"battlefield":["Greed's Gambit"],"library":["Forest","Forest","Forest","Forest"]},"p1":{"battlefield":["Grizzly Bears"]}},"steps":[]}`)
	snap := res.Snapshots[0]
	if setupPermCount(snap, "Greed's Gambit") != 1 {
		t.Fatalf("Greed's Gambit is not on the battlefield at setup: %+v", snap.Permanents)
	}
	if snap.Players[0].Life != 20 {
		t.Errorf("p0 life = %d after setup, want 20 (ETB gained life)", snap.Players[0].Life)
	}
	if h := snap.Players[0].Hand; len(h) != 0 {
		t.Errorf("p0 hand = %v after setup, want empty (ETB drew)", h)
	}

	// Bristlebud Farmer: ETB creates two Food.
	res, _ = runFixtureScenario(t, `{"name":"farmer","setup":{"p0":{"battlefield":["Bristlebud Farmer"]},"p1":{"battlefield":["Grizzly Bears"]}},"steps":[]}`)
	snap = res.Snapshots[0]
	if setupPermCount(snap, "Bristlebud Farmer") != 1 {
		t.Fatalf("Bristlebud Farmer is not on the battlefield at setup: %+v", snap.Permanents)
	}
	for _, p := range snap.Permanents {
		if p.Token {
			t.Errorf("token %s after setup, want none (ETB fired)", p.Name)
		}
	}
}

// Keyword-built and self-damaging ETBs go through the same drop: Hideaway
// (Collector's Cage exiled a card), Legion Extruder's 2 damage to any target,
// Vaultborn Tyrant's gain-3-and-draw.
func TestOracleSetupPlacementFiresNoETBShapes(t *testing.T) {
	for _, card := range []string{"Collector's Cage", "Legion Extruder", "Vaultborn Tyrant"} {
		raw := `{"name":"shape","setup":{"p0":{"battlefield":["` + card + `"],"library":["Forest","Island","Swamp","Plains","Mountain","Forest"]},"p1":{"battlefield":["Grizzly Bears"]}},"steps":[]}`
		res, run := runFixtureScenario(t, raw)
		snap := res.Snapshots[0]
		if setupPermCount(snap, card) != 1 {
			t.Fatalf("%s is not on the battlefield at setup", card)
		}
		p0, p1 := snap.Players[0], snap.Players[1]
		if p0.Life != 20 || p1.Life != 20 || len(p0.Hand) != 0 || len(p0.Exile) != 0 || len(p0.Graveyard) != 0 || len(snap.Stack) != 0 {
			t.Errorf("%s: setup checkpoint moved: life %d/%d hand %v exile %v gy %v stack %v\n%s",
				card, p0.Life, p1.Life, p0.Hand, p0.Exile, p0.Graveyard, snap.Stack, strings.Join(res.Transcript, "\n"))
		}
		_ = run
	}
}

// A trigger that is not an ETB of a setup permanent still fires: Bitterblossom's
// upkeep token (the level-B phase-trigger template).
func TestOracleSetupKeepsPhaseTrigger(t *testing.T) {
	_, run := runFixtureScenario(t, `{"name":"phase","setup":{"p0":{"battlefield":["Bitterblossom"]},"p1":{"battlefield":["Grizzly Bears"]}},"steps":[]}`)
	tokens := 0
	for _, id := range run.e.G.Zone(state.ZBattlefield, 0) {
		if run.e.G.Obj(id).IsToken {
			tokens++
		}
	}
	if tokens != 1 {
		t.Fatalf("p0 has %d tokens, want 1 from the upkeep trigger", tokens)
	}
}
