package templates

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// xabilityCases is one generated item per cause the self-cause recipes serve
// whose steps carry an activate op: the transform recipes' mana taps (the
// component-bearing may-pay pools its mana first) and the cycled recipe's
// own hand cycling activation. Every activate step must carry its XMage
// rule text at its own index and that text must be the named card's OWN
// activation text -- the runner throws on an activate step with an empty
// xmage_ability, and a misaligned parallel slice would activate whatever a
// neighbour's text asks.
var xabilityCases = []struct{ name, key, family string }{
	{"Ashling, Rekindled", "trigger#0.1", "transform"},
	{"Ashling, Rekindled", "trigger#1.0", "transform"},
	{"Brigid, Clachan's Heart", "trigger#0.1", "transform"},
	{"Sygg, Wanderwine Wisdom", "trigger#1.0", "transform"},
	{"Trystan, Callous Cultivator", "trigger#1.0", "transform"},
	{"Sidequest: Raise a Chocobo", "trigger#1.0", "transform"},
	{"Ultimecia, Time Sorceress", "trigger#1.0", "transform"},
	{"Agonasaur Rex", "trigger#0.0", "cycled"},
	{"Webstrike Elite", "trigger#0.0", "cycled"},
	{"Basri, Tomorrow's Champion", "trigger#0.0", "cycled"},
}

// TestSelfCauseActivateStepsCarryXAbility walks every activate step of each
// served self-cause item and asserts it names its own rule text: non-empty
// at the step's index, and equal to XMageAbility's text for exactly the
// face and ability the step asks for.
func TestSelfCauseActivateStepsCarryXAbility(t *testing.T) {
	reg := loadGenRegistry(t)
	transformActs := 0
	for _, tc := range xabilityCases {
		t.Run(tc.name+"/"+tc.key, func(t *testing.T) {
			c, ok := reg.Lookup(tc.name)
			if !ok {
				t.Fatalf("%s not in the corpus", tc.name)
			}
			sub := ""
			face := (*cards.Face)(nil)
			found := false
			for _, r := range levelb.Requirements(c) {
				if r.Key != tc.key {
					continue
				}
				found, sub = true, r.Sub
				face = c.Faces[r.Face]
			}
			if !found {
				t.Fatalf("precondition: %s carries no requirement %s", tc.name, tc.key)
			}
			it := triggerRequirement(t, reg, tc.name, tc.key, sub)
			acts := 0
			for i, st := range it.Scenario.Steps {
				if st.Op != "activate" {
					continue
				}
				acts++
				if i >= len(it.XAbility) || it.XAbility[i] == "" {
					t.Fatalf("activate step %d carries no xmage_ability (XAbility len %d, %d steps)",
						i, len(it.XAbility), len(it.Scenario.Steps))
				}
				ref := strings.TrimPrefix(st.Card, "p0:")
				ref = strings.SplitN(ref, "#", 2)[0]
				wantFace := face
				if !strings.EqualFold(ref, tc.name) {
					lc, ok := reg.Lookup(ref)
					if !ok {
						t.Fatalf("step %d names %s, not in the corpus", i, ref)
					}
					wantFace = lc.Faces[0]
				}
				prefixes, why := oraclegen.XMageAbility(wantFace)
				if why != "" {
					t.Fatalf("xmage text for %s: %s", ref, why)
				}
				want := prefixes[0]
				if st.AbilityIndex != nil {
					if *st.AbilityIndex < 0 || *st.AbilityIndex >= len(prefixes) {
						t.Fatalf("step %d ability index %d beyond %d prefixes", i, *st.AbilityIndex, len(prefixes))
					}
					want = prefixes[*st.AbilityIndex]
				}
				if it.XAbility[i] != want {
					t.Fatalf("step %d xmage_ability %q, want %s's own %q", i, it.XAbility[i], ref, want)
				}
			}
			if tc.family == "cycled" && acts == 0 {
				t.Fatalf("precondition: the cycled item carries no activate step: %+v", it.Scenario.Steps)
			}
			transformActs += acts
		})
	}
	if transformActs == 0 {
		t.Fatalf("precondition: no transform item carries a mana tap to align")
	}
}
