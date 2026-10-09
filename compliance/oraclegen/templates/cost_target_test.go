package templates

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// TestCostStaticTargetFixtures covers the cost statics whose reduction is in
// force only when the probe supplies a matching target or pays an additional
// cost. Each case must generate (not skip) and replay at the reduced price:
// the strict mana string is the evidence the reduction applied -- the printed
// price would not be payable otherwise.
func TestCostStaticTargetFixtures(t *testing.T) {
	reg := loadGenRegistry(t)
	for _, tc := range []struct {
		name string
		// mana is the reduced pool the probe must be cast with.
		mana string
		// precast is true when a spell must be cast first to give the probe a
		// stack target (Out of Air); bargain is true when the cast elects the
		// Bargain mode and sacrifices an artifact (Ice Out).
		precast, bargain bool
	}{
		{name: "Swampsnare Trap"},
		{name: "Grow Extra Arms"},
		{name: "Depower"},
		{name: "Out of Air", mana: "UU", precast: true},
		{name: "Champions of the Perfect"},
		{name: "Ice Out", mana: "UU", precast: true, bargain: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			it := staticCostItem(t, reg, tc.name)
			cast := probeCast(t, it)
			if cast.Mana == "" {
				t.Fatalf("precondition: %s probe has no reduced-price payment", tc.name)
			}
			if tc.mana != "" && cast.Mana != tc.mana {
				t.Fatalf("%s probe mana = %q, want %q (the reduced price)", tc.name, cast.Mana, tc.mana)
			}
			if _, ok := oraclegen.PlaysThrough(reg, it.Scenario); !ok {
				t.Fatalf("precondition: %s reduced-price scenario does not play through: %+v", tc.name, it.Steps)
			}
			if tc.precast {
				// The probe targets a spell on the stack, so a precast must
				// precede it and the probe's target must name the precast.
				var pre *oraclegen.Step
				for i := range it.Steps {
					if it.Steps[i].Op == "cast" && it.Steps[i].Card != cast.Card {
						pre = &it.Steps[i]
					}
				}
				if pre == nil {
					t.Fatalf("precondition: %s probe has no precast spell: %+v", tc.name, it.Steps)
				}
				if len(cast.Targets) == 0 || cast.Targets[0] != pre.Card {
					t.Fatalf("precondition: %s target %v does not name the precast %q", tc.name, cast.Targets, pre.Card)
				}
			}
			if tc.bargain {
				if cast.CastMode != "bargained" {
					t.Fatalf("precondition: %s CastMode = %q, want bargained", tc.name, cast.CastMode)
				}
				found := false
				for _, a := range cast.Answers {
					if a.Kind == "choose" {
						found = true
					}
				}
				if !found {
					t.Fatalf("precondition: %s has no bargain sacrifice answer: %+v", tc.name, cast.Answers)
				}
			}
			if tc.name == "Swampsnare Trap" || tc.name == "Grow Extra Arms" || tc.name == "Depower" {
				if len(cast.Targets) == 0 || cast.Targets[0] == "p1" {
					t.Fatalf("precondition: %s target is not a fixture permanent: %+v", tc.name, cast.Targets)
				}
				owner := strings.SplitN(cast.Targets[0], ":", 2)[0]
				ref := strings.TrimPrefix(cast.Targets[0], owner+":")
				found := false
				for _, card := range it.Scenario.Setup[owner].Battlefield {
					found = found || card == ref
				}
				if !found {
					t.Fatalf("precondition: target %q is absent from %s battlefield", cast.Targets[0], owner)
				}
			}
			if tc.name == "Champions of the Perfect" {
				found := false
				for _, card := range it.Scenario.Setup["p0"].Hand {
					c, ok := reg.Lookup(card)
					if ok && len(c.Faces) > 0 {
						for _, typ := range c.Faces[0].Types {
							found = found || strings.EqualFold(typ, "Elf")
						}
					}
				}
				if !found {
					t.Fatalf("precondition: Champions of the Perfect has no Elf in hand: %v", it.Scenario.Setup["p0"].Hand)
				}
			}
		})
	}
}
