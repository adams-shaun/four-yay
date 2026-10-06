package templates

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// TestClassLevelPrelude pins the one shared Class level-up prelude (CR 716):
// every Class-granted family -- trigger, static, cost static, and the level-3
// activator -- first raises the Class through its own level-up abilities 2..N,
// each followed by resolve, and the emitted item carries XMage's "{cost}:
// Level N" selector on every prelude activation as well as on the probe.
//
// Each row generates through GenerateB, so a row no longer served fails here.
// The scenarios are replayed with PlaysThrough, so an item whose steps gorge
// cannot play is not asserted. A row also asserts the PRECONDITION its claim
// depends on: the card is on p0's battlefield (or the class is at level 1) and
// the level-up activator exists, so a vacuous setup fails loudly.
func TestClassLevelPrelude(t *testing.T) {
	reg := testutil.CorpusRegistry(t)

	requirement := func(t *testing.T, name, key string) levelb.Requirement {
		t.Helper()
		c, ok := reg.Lookup(name)
		if !ok || len(c.Faces) == 0 {
			t.Fatalf("precondition: %s not in the corpus", name)
		}
		for _, r := range levelb.Requirements(c) {
			if r.Key == key {
				return r
			}
		}
		t.Fatalf("precondition: %s carries no requirement %s", name, key)
		return levelb.Requirement{}
	}
	generate := func(t *testing.T, name, key string) oraclegen.Item {
		t.Helper()
		it, skip := GenerateB(reg, name, requirement(t, name, key))
		if skip != nil {
			t.Fatalf("%s %s: skipped: %s", name, key, skip.Reason)
		}
		if _, ok := oraclegen.PlaysThrough(reg, it.Scenario); !ok {
			t.Fatalf("%s %s: scenario does not replay through gorge", name, key)
		}
		return it
	}
	// activatePrefixes returns the XMage selectors of the item's activate
	// steps, in step order, and asserts XAbility is parallel to Steps.
	xabilityParallel := func(t *testing.T, name string, it oraclegen.Item) {
		t.Helper()
		if len(it.XAbility) != len(it.Scenario.Steps) {
			t.Fatalf("%s: xmage_ability has %d entries for %d steps", name, len(it.XAbility), len(it.Scenario.Steps))
		}
		for i, st := range it.Scenario.Steps {
			if st.Op == "activate" && it.XAbility[i] == "" {
				t.Fatalf("%s step %d is an activate with no xmage_ability", name, i)
			}
		}
	}
	// classLevelAtSetup asserts the card starts at level 1 on p0's battlefield,
	// the precondition the prelude exists to change.
	classLevelAtSetup := func(t *testing.T, name string, it oraclegen.Item) {
		t.Helper()
		onBoard := false
		for _, c := range it.Scenario.Setup["p0"].Battlefield {
			if c == name {
				onBoard = true
				break
			}
		}
		// A static or cost static may cast the card instead of placing it; in
		// that case the cast step must exist.
		if !onBoard {
			cast := false
			for _, st := range it.Scenario.Steps {
				if st.Op == "cast" && st.Card == "p0:"+name {
					cast = true
					break
				}
			}
			if !cast {
				t.Fatalf("precondition: %s is neither placed nor cast on p0's side", name)
			}
		}
	}

	t.Run("Bandit's Talent level-2 phase trigger fires", func(t *testing.T) {
		it := generate(t, "Bandit's Talent", "trigger#0.1")
		classLevelAtSetup(t, "Bandit's Talent", it)
		xabilityParallel(t, "Bandit's Talent", it)
		// The level-2 grant (ClassBand$ 2) is live only after the level-2
		// activation, which must be the first step with its selector.
		if len(it.Scenario.Steps) == 0 || it.Scenario.Steps[0].Op != "activate" {
			t.Fatalf("level-2 trigger: first step = %+v, want the level-up activate", it.Scenario.Steps)
		}
		if it.XAbility[0] != "{B}: Level 2" {
			t.Fatalf("level-2 prelude selector = %q, want %q", it.XAbility[0], "{B}: Level 2")
		}
	})

	t.Run("Bandit's Talent level-3 phase trigger fires", func(t *testing.T) {
		it := generate(t, "Bandit's Talent", "trigger#0.2")
		classLevelAtSetup(t, "Bandit's Talent", it)
		xabilityParallel(t, "Bandit's Talent", it)
		// Both activations are needed: level 2 then level 3, in order, each
		// with its own selector.
		want := []string{"{B}: Level 2", "{3}{B}: Level 3"}
		var got []string
		for i, st := range it.Scenario.Steps {
			if st.Op == "activate" {
				got = append(got, it.XAbility[i])
			}
		}
		if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
			t.Fatalf("level-3 prelude selectors = %v, want %v", got, want)
		}
	})

	t.Run("Stormchaser's ClassLevelGained trigger fires", func(t *testing.T) {
		it := generate(t, "Stormchaser's Talent", "trigger#0.1")
		classLevelAtSetup(t, "Stormchaser's Talent", it)
		xabilityParallel(t, "Stormchaser's Talent", it)
		if len(it.Scenario.Steps) == 0 || it.Scenario.Steps[0].Op != "activate" {
			t.Fatalf("ClassLevelGained: first step = %+v, want the level-up activate", it.Scenario.Steps)
		}
		if it.XAbility[0] != "{3}{U}: Level 2" {
			t.Fatalf("ClassLevelGained selector = %q, want %q", it.XAbility[0], "{3}{U}: Level 2")
		}
		// The trigger's target (an instant/sorcery in the graveyard) must be
		// the recorded Shock, proving the cause actually reached level 2 and
		// the trigger resolved a real target.
		target := false
		for _, st := range it.Scenario.Steps {
			for _, a := range st.Answers {
				for _, pick := range a.Pick {
					if pick == "Shock (a)" {
						target = true
					}
				}
			}
		}
		if !target {
			t.Fatalf("ClassLevelGained scenario answers = %+v, want a target pick of the graveyard Shock", it.Scenario.Steps)
		}
	})

	t.Run("Ninja Teen level-2 static is served", func(t *testing.T) {
		it := generate(t, "Ninja Teen", "static#0.0")
		classLevelAtSetup(t, "Ninja Teen", it)
		xabilityParallel(t, "Ninja Teen", it)
		// The level-2 static (creatures you control get +1/+0 and menace) is
		// live only after the level-2 activation.
		want := "{1}{B}: Level 2"
		found := false
		for i, st := range it.Scenario.Steps {
			if st.Op == "activate" && it.XAbility[i] == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("Ninja Teen static: no level-2 activation with selector %q (xability %v)", want, it.XAbility)
		}
	})

	t.Run("Artist's Talent cost static is served", func(t *testing.T) {
		it := generate(t, "Artist's Talent", "static#0.0")
		classLevelAtSetup(t, "Artist's Talent", it)
		xabilityParallel(t, "Artist's Talent", it)
		// The level-2 reduction ({1} off a noncreature spell) only applies
		// after the Class reaches level 2; the probe cast must follow it.
		want := "{2}{R}: Level 2"
		activateAt, castAt := -1, -1
		for i, st := range it.Scenario.Steps {
			if st.Op == "activate" && it.XAbility[i] == want {
				activateAt = i
			}
			if st.Op == "cast" {
				castAt = i
			}
		}
		if activateAt < 0 {
			t.Fatalf("Artist's Talent cost static: no level-2 activation with selector %q (xability %v)", want, it.XAbility)
		}
		if castAt < 0 || castAt < activateAt {
			t.Fatalf("Artist's Talent cost static: probe cast at %d, level-up at %d; the prelude must precede the probe", castAt, activateAt)
		}
	})

	t.Run("Bandit's Talent level-3 activator is served with selectors", func(t *testing.T) {
		it := generate(t, "Bandit's Talent", "activate#0.1")
		classLevelAtSetup(t, "Bandit's Talent", it)
		xabilityParallel(t, "Bandit's Talent", it)
		// The probe is the level-3 activator; the prelude is the level-2
		// activation. Both carries must have XAbility set.
		var activates []int
		for i, st := range it.Scenario.Steps {
			if st.Op == "activate" {
				activates = append(activates, i)
			}
		}
		if len(activates) != 2 {
			t.Fatalf("level-3 activator: %d activate steps, want 2 (level-2 prelude + probe)", len(activates))
		}
		prelude, probe := activates[0], activates[1]
		if it.XAbility[prelude] != "{B}: Level 2" {
			t.Fatalf("prelude selector = %q, want %q", it.XAbility[prelude], "{B}: Level 2")
		}
		if it.XAbility[probe] != "{3}{B}: Level 3" {
			t.Fatalf("probe selector = %q, want %q", it.XAbility[probe], "{3}{B}: Level 3")
		}
		// The probe must be the level-3 ability (IR index 1), not the level-2
		// prelude ability (index 0).
		if it.Scenario.Steps[probe].AbilityIndex == nil || *it.Scenario.Steps[probe].AbilityIndex != 1 {
			t.Fatalf("probe ability_index = %v, want 1", it.Scenario.Steps[probe].AbilityIndex)
		}
	})

	// classLevelPrelude fails closed: a face whose XMage selector is ambiguous
	// returns ok=false, so every caller keeps its named skip.
	t.Run("prelude fails closed without level-up abilities", func(t *testing.T) {
		if _, _, ok := classLevelPrelude(&cards.Face{Name: "No Such Class"}, "No Such Class", 2); ok {
			t.Fatal("classLevelPrelude returned ok for a face with no level-up ability")
		}
	})
}
