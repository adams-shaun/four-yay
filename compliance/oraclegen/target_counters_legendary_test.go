package oraclegen

import "testing"

// A "target legendary creature" slot (Yoshimaru, Beloved Companion) gets a
// legendary creature; none of the generic creature candidates is one.
func TestLegendaryCreatureCandidate(t *testing.T) {
	cs := candidatesFor(nil, "Creature.Legendary", "")
	if len(cs) != 1 || cs[0].card != "Isamaru, Hound of Konda" || cs[0].seat != "p1" || cs[0].zone != "battlefield" {
		t.Fatalf("Creature.Legendary candidates = %+v, want p1's Isamaru, Hound of Konda", cs)
	}
	// A negated predicate is not a demand: nonLegendary keeps the generic list.
	if cs := candidatesFor(nil, "Creature.nonLegendary", ""); len(cs) < 2 || cs[0].card != "Grizzly Bears" {
		t.Fatalf("Creature.nonLegendary candidates = %+v, want the generic creature list", cs)
	}
}

// A "creature with a +1/+1 counter on it" slot (Tomik, Orzhov Lawmage) gets a
// creature whose setup carries that counter, and the fixture's seat says so.
func TestCounterBearingCreatureFixture(t *testing.T) {
	kind, n, ok := counterDemand("Creature.counters_GE1_P1P1")
	if !ok || kind != "P1P1" || n != 1 {
		t.Fatalf("counterDemand = (%q, %d, %v), want (P1P1, 1, true)", kind, n, ok)
	}
	if _, _, ok := counterDemand("Creature.Legendary"); ok {
		t.Fatal("counterDemand found a demand in a filter with none")
	}
	fx := fixtures(nil, []Slot{{Filter: "Creature.counters_GE2_M1M1"}})
	if len(fx) != 1 {
		t.Fatalf("fixtures = %d, want 1", len(fx))
	}
	p1 := fx[0].p1
	if len(p1.Battlefield) != 1 || p1.Battlefield[0] != "Grizzly Bears" {
		t.Fatalf("p1 battlefield = %v, want [Grizzly Bears]", p1.Battlefield)
	}
	if got := p1.Counters["Grizzly Bears"]["M1M1"]; got != 2 {
		t.Fatalf("p1 counters = %v, want Grizzly Bears M1M1=2", p1.Counters)
	}
	if len(fx[0].targets) != 1 || fx[0].targets[0] != "p1:Grizzly Bears" {
		t.Fatalf("targets = %v, want [p1:Grizzly Bears]", fx[0].targets)
	}
	// clone copies the counters map: editing the copy leaves the fixture.
	c := clone(p1)
	c.Counters["Grizzly Bears"]["M1M1"] = 9
	if p1.Counters["Grizzly Bears"]["M1M1"] != 2 {
		t.Fatal("clone shares the counters map with its source")
	}
}
