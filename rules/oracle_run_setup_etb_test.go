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
		res, _ := runFixtureScenario(t, raw)
		snap := res.Snapshots[0]
		if setupPermCount(snap, card) != 1 {
			t.Fatalf("%s is not on the battlefield at setup", card)
		}
		p0, p1 := snap.Players[0], snap.Players[1]
		if p0.Life != 20 || p1.Life != 20 || len(p0.Hand) != 0 || len(p0.Exile) != 0 || len(p0.Graveyard) != 0 || len(snap.Stack) != 0 {
			t.Errorf("%s: setup checkpoint moved: life %d/%d hand %v exile %v gy %v stack %v\n%s",
				card, p0.Life, p1.Life, p0.Hand, p0.Exile, p0.Graveyard, snap.Stack, strings.Join(res.Transcript, "\n"))
		}
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

// A back-face (FlipFace) setup placement must also drop the entry trigger of
// the face it entered on. XMage's addCard(Zone.BATTLEFIELD, transformed)
// places the permanent without running any of its enters-the-battlefield or
// chapter triggers, and a mode-keyed drop (ChangesZone -> Battlefield) misses
// a Saga chapter trigger: it is queued by the battlefield MoveZone, but its
// Mode$ is not TriggerChangesZone. The Legend of Kyoshi's front face is a Saga
// whose chapter I ("draw cards equal to the greatest power among creatures you
// control") fires when the lore counter lands, so the leak is a five-card
// draw from a back-face Avatar Kyoshi (5/4, greatest power 5).
func TestOracleSetupBackFacePlacementFiresNoChapterTrigger(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	c, ok := reg.Lookup("The Legend of Kyoshi")
	if !ok || len(c.Faces) < 2 {
		t.Fatalf("The Legend of Kyoshi is not a >=2-face card in the corpus (faces=%d)", len(c.Faces))
	}
	back := c.Faces[1].Name
	if back == c.Faces[0].Name {
		t.Fatalf("face 1 of The Legend of Kyoshi has the same name as face 0 (%q); the test cannot tell the faces apart", back)
	}

	// A library wide enough that chapter I's draw (X = Avatar Kyoshi's 5 power)
	// is observable in the hand at the setup checkpoint.
	res, _ := runFixtureScenario(t, `{"name":"kyoshi","setup":{"p0":{"battlefield":["The Legend of Kyoshi"],"back_face":["The Legend of Kyoshi"],"library":["Wastes","Wastes","Wastes","Wastes","Wastes","Wastes","Wastes","Wastes"]},"p1":{"battlefield":["Grizzly Bears"]}},"steps":[]}`)
	snap := res.Snapshots[0]
	// Precondition: the placement is on its back face, so the assertions below
	// are about the transformed permanent and cannot pass on a front-face Saga
	// that never flipped (and whose chapter trigger the front face would fire).
	if setupPermCount(snap, back) != 1 {
		t.Fatalf("back face %q is not on the battlefield at setup: %+v", back, snap.Permanents)
	}
	if setupPermCount(snap, "The Legend of Kyoshi") != 0 {
		t.Fatalf("front-face The Legend of Kyoshi is still reported at setup: %+v", snap.Permanents)
	}
	var perm *OracleSnapPerm
	for i := range snap.Permanents {
		if snap.Permanents[i].Name == back {
			perm = &snap.Permanents[i]
		}
	}
	if perm == nil || !oracleHasFold(perm.Types, "Creature") || perm.Controller != 0 {
		t.Fatalf("back face %q is not a p0 creature: %+v", back, snap.Permanents)
	}

	// The chapter I draw must not have happened: XMage's addCard put the card
	// down without firing it, so p0's hand is empty and life is untouched.
	if h := snap.Players[0].Hand; len(h) != 0 {
		t.Errorf("p0 hand = %v after setup, want empty (back-face chapter trigger fired)", h)
	}
	if snap.Players[0].Life != 20 {
		t.Errorf("p0 life = %d after setup, want 20", snap.Players[0].Life)
	}
	// The front face's entry lore counter must not be stranded on the
	// non-Saga back face.
	if n, ok := perm.Counters["LORE"]; ok {
		t.Errorf("back-face %q carries LORE = %d, want no lore counter", back, n)
	}
}

// Another permanent's "one or more creatures enter" trigger (ChangesZoneAll)
// is also caused by the setup placement, so it must not fire either: Welcoming
// Vampire draws when a small creature enters, and p0's hand must stay empty.
func TestOracleSetupPlacementFiresNoChangesZoneAllTrigger(t *testing.T) {
	res, _ := runFixtureScenario(t, `{"name":"watcher","setup":{"p0":{"battlefield":["Welcoming Vampire","Grizzly Bears"],"library":["Forest","Forest","Forest"]},"p1":{"battlefield":["Grizzly Bears"]}},"steps":[]}`)
	snap := res.Snapshots[0]
	if setupPermCount(snap, "Welcoming Vampire") != 1 || setupPermCount(snap, "Grizzly Bears") != 2 {
		t.Fatalf("setup permanents missing: %+v", snap.Permanents)
	}
	if h := snap.Players[0].Hand; len(h) != 0 {
		t.Errorf("p0 hand = %v after setup, want empty (Welcoming Vampire's trigger fired)", h)
	}
}

// The setup-entry drop must not swallow a CounterAdded trigger that the entry
// it drops indirectly caused. Ajani Goldmane entering as a planeswalker places
// its loyalty counters INSIDE the same entry fold that queues its own ETB
// trigger, so Inspired Tethermage's "whenever you put one or more loyalty
// counters on a planeswalker" trigger is queued in the same window. XMage's
// addCard places the permanent and its entry counters, and the Tethermage
// trigger fires there (the generated-scenario compliance test
// TestGeneratedSetupKeepsCounterAddedTrigger pins the same board); clearing
// the whole window drops it and this test goes red.
func TestOracleSetupKeepsCounterAddedTrigger(t *testing.T) {
	res, _ := runFixtureScenario(t, `{"name":"tethermage","setup":{"p0":{"battlefield":["Inspired Tethermage","Ajani Goldmane"]},"p1":{"battlefield":["Grizzly Bears"]}},"steps":[]}`)
	snap := res.Snapshots[0]
	var teth *OracleSnapPerm
	for i := range snap.Permanents {
		if snap.Permanents[i].Name == "Inspired Tethermage" {
			teth = &snap.Permanents[i]
		}
	}
	// Precondition: both permanents resolved to the battlefield, so the
	// assertion below is about a real entry fold and not a failed placement.
	if teth == nil || setupPermCount(snap, "Ajani Goldmane") != 1 {
		t.Fatalf("setup permanents = %+v, want Inspired Tethermage and Ajani Goldmane", snap.Permanents)
	}
	if teth.Counters["P1P1"] != 1 || teth.PT != "4/3" {
		t.Fatalf("setup Tethermage = %s counters=%v, want 4/3 with one P1P1 counter (CounterAdded trigger kept)", teth.PT, teth.Counters)
	}
}
