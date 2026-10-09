package templates

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// TestCostStaticRaiseTokenProbes covers the raise-cost rows whose additional
// cost the token table (cost_raise_tokens.go) now pays:
//
//   - Benevolent River Spirit and Water Whip pay Waterbend<5> as five
//     generic in the pool (waterbend lets a player tap artifacts and
//     creatures for {1}, it never requires it, CR 701.67a).
//   - Soul Immolation pays Blight<X> as one -1/-1 counter on the 1/1
//     fixture creature, announced as the cast's X (CR 601.2b, the X cap the
//     card's XMax$ GrTo names).
//   - Close Encounter pays its ChooseCard cost with a creature you control
//     (the battlefield arm; the exile arm stays empty, so the exactly-N
//     auto-settle settles the pick without an ask).
//
// Each row must play through gorge with the static present, and a probe
// whose additional-cost payment is removed must fail while the static is
// there and pass once it is gone, so an engine (or generator) that ignores
// the additional cost cannot pass the observation.
func TestCostStaticRaiseTokenProbes(t *testing.T) {
	reg := loadGenRegistry(t)
	for _, tc := range []struct {
		name, key, mana, printed string
		// creature is the additional-cost fixture the setup must carry (the
		// blight pick and the choose-cost pool are paid from it).
		creature string
		// xAnswer is the cast step's scripted X announcement ("" for none).
		xAnswer string
		// xmageX is the XMage choice XAnswersForScenario derives for it.
		xmageX string
	}{
		{"Benevolent River Spirit", "static#0.0", "UUCCCCC", "UU", "", "", ""},
		{"Water Whip", "static#0.0", "UUCCCCC", "UU", "", "", ""},
		{"Soul Immolation", "static#0.0", "CCCRR", "CCCRR", "Llanowar Elves", "X = 1", "X=1"},
		{"Close Encounter", "static#0.0", "CG", "CG", "Llanowar Elves", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			it := staticCostItem(t, reg, tc.name)
			cast := probeCast(t, it)
			if cast.Card != "p0:"+tc.name || cast.Mana != tc.mana {
				t.Fatalf("probe = %+v paying %q, want cast %s paying %q", cast, cast.Mana, "p0:"+tc.name, tc.mana)
			}
			c, ok := reg.Lookup(tc.name)
			if !ok {
				t.Fatalf("precondition: probe %s absent", tc.name)
			}
			pool, why := oraclegen.PoolFor(c.Faces[0].ManaCost)
			if why != "" || pool != tc.printed {
				t.Fatalf("precondition: %s prints %q (%s), table says %q", tc.name, pool, why, tc.printed)
			}
			if tc.creature != "" {
				found := false
				for _, card := range it.Scenario.Setup["p0"].Battlefield {
					found = found || card == tc.creature
				}
				if !found {
					t.Fatalf("precondition: additional-cost fixture %q absent from p0's battlefield: %v", tc.creature, it.Scenario.Setup["p0"].Battlefield)
				}
			} else if got := strings.Count(cast.Mana, "C") - strings.Count(tc.printed, "C"); got != 5 {
				t.Fatalf("precondition: waterbend probe pays %d generic beyond the printed price, want 5", got)
			}
			if tc.xAnswer != "" {
				found := false
				for _, a := range cast.Answers {
					for _, pick := range a.Pick {
						found = found || pick == tc.xAnswer
					}
				}
				if !found {
					t.Fatalf("precondition: cast answers %v never announce %q", cast.Answers, tc.xAnswer)
				}
				found = false
				for _, step := range it.XAnswers {
					for _, a := range step {
						found = found || (a.Kind == "choice" && a.Value == tc.xmageX)
					}
				}
				if !found {
					t.Fatalf("precondition: XMage answers %v never carry %q", it.XAnswers, tc.xmageX)
				}
			}
			if res := runSteps(t, reg, it.Scenario, it.Steps); len(res.Fails) != 0 {
				t.Fatalf("additional-cost probe fails with the static present: %v", res.Fails)
			}
			// Sensitivity: with the additional-cost payment removed, the exact
			// cast must fail while the static is present and succeed once the
			// static is gone. The waterbend rows drop the five generic from
			// the pool; the pick-based rows drop the paying creature (and the
			// X announcement whose cap it bounds).
			muted := muteProbeCast(it.Scenario, tc.name, tc.printed, tc.creature, tc.xAnswer)
			if res := runSteps(t, reg, muted, muted.Steps); len(res.Fails) == 0 {
				t.Fatalf("probe is not sensitive to %s's additional cost", tc.name)
			}
			if res := runSteps(t, withoutStatics(reg, tc.name), muted, muted.Steps); len(res.Fails) != 0 {
				t.Fatalf("muted probe still fails with the static gone: %v", res.Fails)
			}
		})
	}
}

// muteProbeCast removes the additional-cost payment from sc: the waterbend
// rows pay the printed price only, the pick-based rows lose the paying
// creature and every scripted cast answer the payment asked for.
func muteProbeCast(sc oraclegen.Scenario, name, printed, creature, xAnswer string) oraclegen.Scenario {
	out := sc
	out.Setup = map[string]oraclegen.Seat{
		"p0": sc.Setup["p0"],
		"p1": sc.Setup["p1"],
	}
	if creature != "" {
		p0 := out.Setup["p0"]
		bf := make([]string, 0, len(p0.Battlefield))
		for _, card := range p0.Battlefield {
			if card != creature {
				bf = append(bf, card)
			}
		}
		p0.Battlefield = bf
		out.Setup["p0"] = p0
	}
	out.Steps = append([]oraclegen.Step(nil), sc.Steps...)
	for i, s := range out.Steps {
		if s.Op != "cast" || s.Card != "p0:"+name {
			continue
		}
		if creature == "" {
			s.Mana = printed
		}
		if xAnswer != "" {
			s.Answers = nil
		}
		out.Steps[i] = s
	}
	return out
}
