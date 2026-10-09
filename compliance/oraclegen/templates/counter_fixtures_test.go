package templates

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
)

// counterReq finds name's level-B requirement for key (any sub-family).
func counterReq(t *testing.T, reg *cards.Registry, name, key string) levelb.Requirement {
	t.Helper()
	c, ok := reg.Lookup(name)
	if !ok {
		t.Fatalf("%s not in the corpus", name)
	}
	for _, r := range levelb.Requirements(c) {
		if r.Key == key {
			return r
		}
	}
	t.Fatalf("precondition: %s carries no requirement %s", name, key)
	return levelb.Requirement{}
}

// TestStaticCounterGateStation: Dawnsire's STATION 20+ static turns the
// Spacecraft into a flying artifact creature once it holds 20 charge counters.
// The scenario starts it holding exactly 20, so the first checkpoint shows the
// creature; it is not a creature as printed, which the precondition pins.
func TestStaticCounterGateStation(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	const name = "Dawnsire, Sunstar Dreadnought"
	c, _ := reg.Lookup(name)
	if c.Faces[0].IsCreature() {
		t.Fatalf("precondition: %s is printed as a creature", name)
	}
	it, skip := GenerateB(reg, name, counterReq(t, reg, name, "static#0.1"))
	if skip != nil {
		t.Fatalf("static#0.1: %s", skip.Reason)
	}
	if got := it.Setup["p0"].Counters[name]["CHARGE"]; got != 20 {
		t.Fatalf("p0 counters = %v, want 20 CHARGE on %s", it.Setup["p0"].Counters, name)
	}
	res, err := rules.RunOracleScenarioJSON(reg, it.Raw())
	if err != nil || len(res.Fails) != 0 || len(res.Snapshots) == 0 {
		t.Fatalf("scenario does not replay: err=%v fails=%v", err, res.Fails)
	}
	found := false
	for _, p := range res.Snapshots[len(res.Snapshots)-1].Permanents {
		if p.Name != name {
			continue
		}
		found = true
		if p.Counters["CHARGE"] != 20 || !hasString(p.Types, "Creature") {
			t.Fatalf("%s snapshot: counters %v types %v, want 20 CHARGE and Creature", name, p.Counters, p.Types)
		}
	}
	if !found {
		t.Fatalf("%s is not on the battlefield in the last snapshot", name)
	}
}

// TestStaticCounterGateGrantUsesNamedTriggerGap: the 10+ station static only
// grants a trigger, which the granted-trigger observation serves by firing it
// (static_granted_trigger.go) from the gate-on fixture: the card placed with
// the gate's ten counters.
func TestStaticCounterGateGrantUsesNamedTriggerGap(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	const name = "Dawnsire, Sunstar Dreadnought"
	it, skip := GenerateB(reg, name, counterReq(t, reg, name, "static#0.0"))
	if skip != nil {
		t.Fatalf("static#0.0 skip = %v, want the granted-trigger item", skip)
	}
	if got := it.Setup["p0"].Counters[name]["CHARGE"]; got != 10 {
		t.Fatalf("gate-on fixture holds %d CHARGE counters, want the GE10 gate's 10", got)
	}
}

// TestStaticSelfCounterGate: Super-Soldier-style "as long as it has a counter"
// gates are read from IsPresent$ too, with the kind and count parsed once.
func TestStaticSelfCounterGate(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	c, _ := reg.Lookup("Captain America, Super-Soldier")
	st := &c.Faces[0].Statics[0]
	kind, n, ok := staticCounterGate(st)
	if !ok || kind != "SHIELD" || n != 1 {
		t.Fatalf("staticCounterGate = (%q, %d, %v), want (SHIELD, 1, true)", kind, n, ok)
	}
	c, _ = reg.Lookup("Anthem of Champions")
	if _, _, ok := staticCounterGate(&c.Faces[0].Statics[0]); ok {
		t.Fatal("an ungated anthem must not read as counter-gated")
	}
}

// TestStaticMaxSpeedFixture: a Condition$ MaxSpeed static is recognised and
// its fixture raises p0 to speed 4; a trigger grant gets its named gap.
func TestStaticMaxSpeedFixture(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	const name = "Aether Syphon"
	c, _ := reg.Lookup(name)
	if !staticGatedOnMaxSpeed(&c.Faces[0].Statics[0]) {
		t.Fatalf("precondition: %s static#0.0 is not gated on MaxSpeed", name)
	}
	setup := map[string]oraclegen.Seat{"p0": {Battlefield: []string{name}}, "p1": {}}
	if setup["p0"].Speed != 0 {
		t.Fatal("precondition: a fresh seat starts at speed 0")
	}
	withMaxSpeed(setup)
	if setup["p0"].Speed != 4 {
		t.Fatalf("withMaxSpeed left p0 at speed %d, want 4", setup["p0"].Speed)
	}
	// The MaxSpeed-gated trigger grant is served by firing the granted
	// trigger from the gate-on fixture (static_granted_trigger.go): p0 sits
	// at speed 4 in its scenario.
	it, skip := GenerateB(reg, name, counterReq(t, reg, name, "static#0.0"))
	if skip != nil {
		t.Fatalf("%s static#0.0 skip = %v, want the granted-trigger item", name, skip)
	}
	if it.Setup["p0"].Speed != 4 {
		t.Fatalf("granted-trigger fixture has p0 at speed %d, want the gate's 4", it.Setup["p0"].Speed)
	}
}

// TestActivateSourceCounterCosts: an activation whose cost removes counters from
// its source starts the card holding them, so the activation is payable and the
// scenario replays. M1M1 stands in for "any" (the blight creatures' counter).
func TestActivateSourceCounterCosts(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, tc := range []struct {
		name, key, kind string
		n               int32
	}{
		{"Weather Maker", "activate#0.2", "CHARGE", 3},
		{"Wishclaw Talisman", "activate#0.0", "WISH", 1},
		{"Brambleback Brute", "activate#0.0", "M1M1", 1},
	} {
		it, _ := activateRequirement(t, reg, tc.name, tc.key)
		if got := it.Setup["p0"].Counters[tc.name][tc.kind]; got != tc.n {
			t.Errorf("%s %s: counters = %v, want %d %s", tc.name, tc.key, it.Setup["p0"].Counters, tc.n, tc.kind)
		}
		res, ok := oraclegen.PlaysThrough(reg, it.Scenario)
		if !ok || len(res.Fails) != 0 {
			t.Errorf("%s %s does not play through gorge: ok=%v fails=%v", tc.name, tc.key, ok, res.Fails)
		}
	}
}

// TestSourceCounterCostFailsClosed: a cost that takes counters from another
// permanent, announces X, names a loyalty cost or is not literal stays a gap.
func TestSourceCounterCostFailsClosed(t *testing.T) {
	for _, tok := range []string{
		"SubCounter<1/LOYALTY>", "SubCounter<X/CHARGE>", "SubCounter<1/P1P1/Creature.YouCtrl>",
		"RemoveAnyCounter<1/Any/Creature.YouCtrl>", "SubCounter<0/CHARGE>", "Sac<1/CARDNAME>",
	} {
		if _, _, ok := sourceCounterCost(tok); ok {
			t.Errorf("sourceCounterCost(%q) accepted a shape the fixture cannot place", tok)
		}
	}
	if n, kind, ok := sourceCounterCost("SubCounter<2/CHARGE>"); !ok || n != 2 || kind != "CHARGE" {
		t.Fatalf("SubCounter<2/CHARGE> = (%d, %q, %v)", n, kind, ok)
	}
}
