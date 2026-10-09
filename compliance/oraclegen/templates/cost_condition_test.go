package templates

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// TestCostStaticConditionFixtures: a cost static whose reduction depends on a
// board, a graveyard, a turn history or a speed must be served with a probe
// that makes the condition true. Each row replays through gorge at the reduced
// price, fails at that price once the static is removed, and fails once the
// fixture that makes the condition true is stripped, so neither a reducer that
// ignores the condition nor a probe that satisfies it by accident passes.
func TestCostStaticConditionFixtures(t *testing.T) {
	reg := loadGenRegistry(t)
	cases := []struct {
		name, key string
		// has asserts the fixture the condition needs is in the scenario.
		has func(t *testing.T, sc oraclegen.Scenario)
		// strip removes that fixture, leaving the static in place.
		strip func(sc *oraclegen.Scenario)
	}{
		{"Pearl of Wisdom", "static#0.0", subtypeOnBoard(reg, "Otter", 1), clearBoard},
		{"Arcane Epiphany", "static#0.0", subtypeOnBoard(reg, "Wizard", 1), clearBoard},
		{"Ghalta, Primal Hunger", "static#0.0", subtypeOnBoard(reg, "Creature", 1), clearBoard},
		{"Rime Chill", "static#0.0", subtypeOnBoard(reg, "Creature", 1), clearBoard},
		{"Salt Road Packbeast", "static#0.0", subtypeOnBoard(reg, "Creature", 3), clearBoard},
		{"Diamond Weapon", "static#0.0", func(t *testing.T, sc oraclegen.Scenario) {
			if len(sc.Setup["p0"].Graveyard) == 0 {
				t.Fatalf("precondition: no permanent card in the graveyard: %+v", sc.Setup["p0"])
			}
		}, func(sc *oraclegen.Scenario) {
			p0 := sc.Setup["p0"]
			p0.Graveyard = nil
			sc.Setup["p0"] = p0
		}},
		{"Gigastorm Titan", "static#0.0", func(t *testing.T, sc oraclegen.Scenario) {
			casts := 0
			for _, s := range sc.Steps {
				if s.Op == "cast" {
					casts++
				}
			}
			if casts < 2 {
				t.Fatalf("precondition: want an earlier cast before the probe, got steps %+v", sc.Steps)
			}
		}, func(sc *oraclegen.Scenario) {
			// Drop the earlier cast; keep the probe cast and what follows it.
			for i := len(sc.Steps) - 1; i >= 0; i-- {
				if sc.Steps[i].Op == "cast" {
					sc.Steps = sc.Steps[i:]
					return
				}
			}
		}},
		{"Samut, the Driving Force", "static#0.1", func(t *testing.T, sc oraclegen.Scenario) {
			if sc.Setup["p0"].Speed < 2 {
				t.Fatalf("precondition: p0 speed = %d, want >= 2 (1 is what Start your engines! gives)", sc.Setup["p0"].Speed)
			}
		}, func(sc *oraclegen.Scenario) {
			p0 := sc.Setup["p0"]
			p0.Speed = 0
			sc.Setup["p0"] = p0
		}},
		{"Lashwhip Predator", "static#0.0", func(t *testing.T, sc oraclegen.Scenario) {
			if len(sc.Setup["p1"].Battlefield) < 3 {
				t.Fatalf("precondition: p1 setup %v, want the gate's three fixture creatures", sc.Setup["p1"].Battlefield)
			}
		}, func(sc *oraclegen.Scenario) {
			p1 := sc.Setup["p1"]
			p1.Battlefield = nil
			sc.Setup["p1"] = p1
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			it, skip := GenerateB(reg, tc.name, costRequirement(t, reg, tc.name, tc.key))
			if skip != nil {
				t.Fatalf("skip %q, want a served item", skip.Reason)
			}
			tc.has(t, it.Scenario)
			probe := probeStep(t, it)
			if printed := printedPool(t, reg, probe.Card); len(probe.Mana) >= len(printed) {
				t.Fatalf("precondition: probe pays %q, printed price %q is not reduced", probe.Mana, printed)
			}
			if _, ok := oraclegen.PlaysThrough(reg, it.Scenario); !ok {
				t.Fatal("reduced-price scenario does not play through gorge")
			}
			if _, ok := oraclegen.PlaysThrough(withoutStatics(reg, tc.name), it.Scenario); ok {
				t.Fatal("reduced-price cast still plays through without the static")
			}
			stripped := it.Scenario
			stripped.Setup = map[string]oraclegen.Seat{}
			for k, v := range it.Scenario.Setup {
				stripped.Setup[k] = v
			}
			stripped.Steps = append([]oraclegen.Step(nil), it.Scenario.Steps...)
			tc.strip(&stripped)
			if _, ok := oraclegen.PlaysThrough(reg, stripped); ok {
				t.Fatal("reduced-price cast plays through with the condition fixture stripped")
			}
		})
	}
}

// printedPool is the mana pool paying a card's printed cost.
func printedPool(t *testing.T, reg *cards.Registry, ref string) string {
	t.Helper()
	pool, why := oraclegen.PoolFor(faceOf(t, reg, strings.TrimPrefix(ref, "p0:")).ManaCost)
	if why != "" {
		t.Fatalf("precondition: %s has no payable printed cost: %s", ref, why)
	}
	return pool
}

func clearBoard(sc *oraclegen.Scenario) {
	p0 := sc.Setup["p0"]
	p0.Battlefield = nil
	sc.Setup["p0"] = p0
}

// subtypeOnBoard asserts at least n p0 battlefield cards carry the subtype (or
// card type) word.
func subtypeOnBoard(reg *cards.Registry, word string, n int) func(*testing.T, oraclegen.Scenario) {
	return func(t *testing.T, sc oraclegen.Scenario) {
		t.Helper()
		count := 0
		for _, name := range sc.Setup["p0"].Battlefield {
			c, ok := reg.Lookup(name)
			if !ok {
				continue
			}
			for _, typ := range c.Faces[0].Types {
				if strings.EqualFold(typ, word) {
					count++
					break
				}
			}
		}
		if count < n {
			t.Fatalf("precondition: %d %s on the battlefield, want >= %d: %v", count, word, n, sc.Setup["p0"].Battlefield)
		}
	}
}
