package templates

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// attackStep returns the scenario's attack step, failing when there is none.
func attackStep(t *testing.T, steps []oraclegen.Step) oraclegen.Step {
	t.Helper()
	for _, st := range steps {
		if st.Op == "attack" {
			return st
		}
	}
	t.Fatalf("precondition: scenario has no attack step: %+v", steps)
	return oraclegen.Step{}
}

// cardTypes counts the distinct card types among names.
func cardTypes(t *testing.T, reg *cards.Registry, names []string) int {
	t.Helper()
	seen := map[string]bool{}
	for _, n := range names {
		c, ok := reg.Lookup(n)
		if !ok {
			t.Fatalf("precondition: %s not in the corpus", n)
		}
		for _, ty := range c.Faces[0].Types {
			switch ty {
			case "Creature", "Land", "Instant", "Sorcery", "Artifact", "Enchantment", "Planeswalker", "Battle":
				seen[ty] = true
			}
		}
	}
	return len(seen)
}

// TestTriggerConditionNonPhase: an attack, dies or drawn trigger whose own
// condition the bare cause leaves false is served with the fixture that makes
// it true, and gorge shows its ability on the stack.
func TestTriggerConditionNonPhase(t *testing.T) {
	reg := loadGenRegistry(t)
	for _, tc := range []struct {
		name, key, sub string
		check          func(t *testing.T, p0 oraclegen.Seat, steps []oraclegen.Step)
	}{
		{"Karlov Watchdog", "trigger#0.0", "trigger.attacks", func(t *testing.T, p0 oraclegen.Seat, steps []oraclegen.Step) {
			// "Whenever you attack with three or more creatures".
			atk := attackStep(t, steps).Attackers
			if len(atk) < 3 {
				t.Fatalf("attackers = %v, want three or more", atk)
			}
			for _, a := range atk {
				if !containsString(p0.Battlefield, strings.TrimPrefix(a, "p0:")) {
					t.Fatalf("attacker %s is not on p0's battlefield %v", a, p0.Battlefield)
				}
			}
		}},
		{"Fear of Missing Out", "trigger#0.1", "trigger.attacks", func(t *testing.T, p0 oraclegen.Seat, steps []oraclegen.Step) {
			// Delirium: four or more card types among cards in the graveyard.
			if n := cardTypes(t, reg, p0.Graveyard); n < 4 {
				t.Fatalf("graveyard %v has %d card types, want delirium (4)", p0.Graveyard, n)
			}
		}},
		{"Madame Masque", "trigger#0.1", "trigger.drawn", func(t *testing.T, p0 oraclegen.Seat, steps []oraclegen.Step) {
			// Her own enters-connive resolves at setup and discards the first
			// card in hand, so the draw spell must not be first.
			var cast string
			for _, st := range steps {
				if st.Op == "cast" {
					cast = strings.TrimPrefix(st.Card, "p0:")
				}
			}
			if cast == "" || !containsString(p0.Hand, cast) || p0.Hand[0] == cast {
				t.Fatalf("hand %v: want the cast %q behind a discard filler", p0.Hand, cast)
			}
		}},
		{"The Earth King", "trigger#0.1", "trigger.attacks", func(t *testing.T, p0 oraclegen.Seat, steps []oraclegen.Step) {
			// "Creatures you control with power 4 or greater attack": a
			// 4-power creature attacks.
			found := false
			for _, a := range attackStep(t, steps).Attackers {
				c, ok := reg.Lookup(strings.TrimPrefix(a, "p0:"))
				found = found || (ok && c.Faces[0].PT == "4/5")
			}
			if !found {
				t.Fatalf("no 4-power attacker in %v", attackStep(t, steps).Attackers)
			}
		}},
		{"Fearless Swashbuckler", "trigger#0.0", "trigger.attacks", func(t *testing.T, p0 oraclegen.Seat, steps []oraclegen.Step) {
			// "if a Pirate and a Vehicle attacked this combat": a Pirate and
			// the crewed Vehicle attack. Precondition: both are on p0's
			// battlefield, so both can be declared.
			atk := attackStep(t, steps).Attackers
			if len(atk) != 2 || !containsString(atk, "p0:Kitesail Corsair") || !containsString(atk, "p0:Veloheart Bike") {
				t.Fatalf("attackers = %v, want a Pirate and a Vehicle", atk)
			}
			for _, a := range atk {
				if !containsString(p0.Battlefield, strings.TrimPrefix(a, "p0:")) {
					t.Fatalf("attacker %s is not on p0's battlefield %v", a, p0.Battlefield)
				}
			}
			// The Vehicle is not a creature until it is crewed: an activate
			// step names it, and its crew taps the source.
			crewed := false
			for _, st := range steps {
				if st.Op == "activate" && st.Card == "p0:Veloheart Bike" {
					crewed = true
				}
			}
			if !crewed {
				t.Fatalf("Veloheart Bike is never crewed: %+v", steps)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			it := triggerRequirement(t, reg, tc.name, tc.key, tc.sub)
			p0 := it.Scenario.Setup["p0"]
			if !containsString(p0.Battlefield, tc.name) {
				t.Fatalf("precondition: %s is not on p0's battlefield %v", tc.name, p0.Battlefield)
			}
			tc.check(t, p0, it.Scenario.Steps)
			if res, ok := oraclegen.PlaysThrough(reg, it.Scenario); !ok || len(res.Fails) != 0 {
				t.Fatalf("does not play through gorge: ok=%v fails=%v", ok, res.Fails)
			}
			if !triggerShownOnStack(t, reg, it.Scenario, tc.name) {
				t.Fatalf("no snapshot shows an ability on the stack sourced by %s", tc.name)
			}
		})
	}
}

// TestTriggerConditionSkipsAreNamed: a condition no probe can make true is a
// named skip, not the bare "did not fire".
func TestTriggerConditionSkipsAreNamed(t *testing.T) {
	reg := loadGenRegistry(t)
	// The table is empty: every named condition skip this test tracked is now
	// served. Clandestine Meddler and Tolsimir, Midnight's Light moved to the
	// served side (trigger_attacker_property_test.go), and Fearless
	// Swashbuckler's indirect Pirate/Vehicle SVar gate is served by
	// trigger_condition_indirect.go (TestTriggerConditionNonPhase).
	for _, tc := range []struct{ name, key, want string }{} {
		t.Run(tc.name, func(t *testing.T) {
			c, ok := reg.Lookup(tc.name)
			if !ok {
				t.Fatalf("%s not in the corpus", tc.name)
			}
			for _, r := range levelb.Requirements(c) {
				if r.Key != tc.key {
					continue
				}
				_, skip := GenerateB(reg, tc.name, r)
				if skip == nil || skip.Reason != tc.want {
					t.Fatalf("%s %s: skip = %v, want %q", tc.name, tc.key, skip, tc.want)
				}
				return
			}
			t.Fatalf("precondition: %s has no requirement %s", tc.name, tc.key)
		})
	}
}
