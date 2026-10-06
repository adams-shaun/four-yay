package templates

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// A counter-gated static on a card with its own enters-the-battlefield trigger
// stays on the cast path. XMage cheats setup permanents onto the battlefield
// before the game starts (ScenarioReplay's clearSetupEntryHistory comment), so
// it never fires their ETB, while gorge's runner places them with a MoveZone
// event that does. Staging the static by setup-placing the card therefore makes
// the two engines diverge (Atmospheric Greenhouse: gorge 6/5 with its own +1/+1
// counter, XMage 5/4 without). The cast path fires the trigger in both engines.
func TestCounterGatedStaticETBUsesCastPath(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	c, ok := reg.Lookup("Atmospheric Greenhouse")
	if !ok || len(c.Faces) == 0 {
		t.Fatal("precondition: Atmospheric Greenhouse not in the corpus")
	}
	req := requirementByKey(t, reg, "Atmospheric Greenhouse", "static#0.0")
	st, _ := staticSlotOf(c.Faces[0], req)
	if !staticCounterGated(&st) {
		t.Fatalf("precondition: Atmospheric Greenhouse static#0.0 is not counter-gated")
	}
	if !staticSelfETB(c.Faces[0]) {
		t.Fatalf("precondition: Atmospheric Greenhouse has no self-ETB trigger")
	}
	it, skip := GenerateB(reg, "Atmospheric Greenhouse", req)
	if skip != nil {
		t.Fatalf("static#0.0 skipped: %q", skip.Reason)
	}
	if got := it.Scenario.Setup["p0"].Counters; len(got) != 0 {
		t.Errorf("setup counters = %v, want none (the cast path stages no counters)", got)
	}
	foundCast := false
	for _, s := range it.Scenario.Steps {
		foundCast = foundCast || s.Op == "cast"
	}
	if !foundCast {
		t.Errorf("scenario steps = %v, want a cast", it.Scenario.Steps)
	}
}

// The same gate on a card with no ETB of its own keeps the counter setup: the
// driver's applyEffects recompute makes XMage apply the counter-gated static
// (Myojin's indestructible), so the two engines agree.
func TestCounterGatedStaticWithoutETBUsesSetupCounters(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	c, ok := reg.Lookup("Myojin of Night's Reach")
	if !ok || len(c.Faces) == 0 {
		t.Fatal("precondition: Myojin of Night's Reach not in the corpus")
	}
	if staticSelfETB(c.Faces[0]) {
		t.Fatalf("precondition: Myojin of Night's Reach has a self-ETB trigger")
	}
	req := requirementByKey(t, reg, "Myojin of Night's Reach", "static#0.0")
	st, _ := staticSlotOf(c.Faces[0], req)
	if !staticCounterGated(&st) {
		t.Fatalf("precondition: Myojin static#0.0 is not counter-gated")
	}
	it, skip := GenerateB(reg, "Myojin of Night's Reach", req)
	if skip != nil {
		t.Fatalf("static#0.0 skipped: %q", skip.Reason)
	}
	if got := it.Scenario.Setup["p0"].Counters["Myojin of Night's Reach"]["DIVINITY"]; got != 1 {
		t.Errorf("setup divinity counters = %d, want 1 (the counter setup)", got)
	}
}

// The helper's two answers both occur among the cards this fixture uses, so a
// broken or absent implementation cannot pass it vacuously.
func TestStaticSelfETBDetection(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	cases := map[string]bool{
		"Atmospheric Greenhouse":  true,  // "When this Spacecraft enters, ..."
		"Kefka, Court Mage":       true,  // "Whenever NICKNAME enters or attacks, ..."
		"Myojin of Night's Reach": false, // static only
	}
	for name, want := range cases {
		c, ok := reg.Lookup(name)
		if !ok || len(c.Faces) == 0 {
			t.Fatalf("%s not in the corpus", name)
		}
		got := false
		for _, f := range c.Faces {
			got = got || staticSelfETB(f)
		}
		if got != want {
			t.Errorf("staticSelfETB(%s) = %v, want %v", name, got, want)
		}
	}
}

// staticRequirement finds a card's requirement by key, failing the test with
// the keys it does have.
func requirementByKey(t *testing.T, reg *cards.Registry, name, key string) levelb.Requirement {
	t.Helper()
	c, ok := reg.Lookup(name)
	if !ok {
		t.Fatalf("%s not in the corpus", name)
	}
	for _, req := range levelb.Requirements(c) {
		if req.Key == key {
			return req
		}
	}
	t.Fatalf("%s has no requirement %s", name, key)
	return levelb.Requirement{}
}
