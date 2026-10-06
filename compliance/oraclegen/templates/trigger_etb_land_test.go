package templates

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// TestTriggerETBLand: a land's own enters-the-battlefield trigger fires when
// the land is PLAYED from hand, not cast and not placed by setup. The item
// must put the card in p0's hand (never on the battlefield), play it with the
// runner's `play` op, and show the card's trigger on the stack, so gorge's own
// engine decided the play fired it. Each row names the trigger's Execute API
// so a wrong card cannot pass as the intended shape.
func TestTriggerETBLand(t *testing.T) {
	reg := loadGenRegistry(t)
	for _, tc := range []struct{ name, key, execute string }{
		// MSH's gain-life lands, including one whose own ETB is a separate
		// "enters tapped" replacement.
		{"A.I.M. Labs", "trigger#0.0", "TrigGainLife"},
		{"Avengers Hangar", "trigger#0.0", "TrigGainLife"},
		// A scry ETB is a mid-resolution choice, not a plain effect.
		{"Temple of Abandon", "trigger#0.0", "TrigScry"},
		// The common gain-life dual land, shared across sets.
		{"Bloodfell Caves", "trigger#0.0", "TrigGainLife"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, ok := reg.Lookup(tc.name)
			if !ok {
				t.Fatalf("%s not in the corpus", tc.name)
			}
			// Precondition: the card is a land whose named trigger carries the
			// effect this row asserts, so a wrong card cannot pass silently.
			if !faceHasType(t, reg, tc.name, "Land") {
				t.Fatalf("precondition: %s is not a land", tc.name)
			}
			found := false
			for i := range c.Faces[0].Triggers {
				if c.Faces[0].Triggers[i].ParamStr(cards.PKExecute) == tc.execute {
					found = true
				}
			}
			if !found {
				t.Fatalf("precondition: %s has no trigger executing %s", tc.name, tc.execute)
			}

			// triggerRequirement asserts the requirement's sub-family.
			it := triggerRequirement(t, reg, tc.name, tc.key, "trigger.etb-land")

			// Precondition: the card starts in hand, not placed on the
			// battlefield, so the play is what moves it.
			p0 := it.Scenario.Setup["p0"]
			if !sliceHas(p0.Hand, tc.name) {
				t.Fatalf("precondition: %s not in p0's hand: %v", tc.name, p0.Hand)
			}
			if sliceHas(p0.Battlefield, tc.name) {
				t.Fatalf("precondition: %s is on p0's battlefield: %v", tc.name, p0.Battlefield)
			}

			steps := it.Scenario.Steps
			if len(steps) == 0 || steps[0].Op != "play" || steps[0].Card != "p0:"+tc.name {
				t.Fatalf("first step = %+v, want play of p0:%s", steps, tc.name)
			}
			if res, ok := oraclegen.PlaysThrough(reg, it.Scenario); !ok || len(res.Fails) != 0 {
				t.Fatalf("does not play through gorge: ok=%v fails=%v", ok, res.Fails)
			}
			if !triggerProbeSlotOnStack(t, reg, it.Scenario, tc.name, "0") {
				t.Fatalf("no snapshot shows an ability on the stack sourced by %s", tc.name)
			}
			t.Logf("%s: play %s (%s)", tc.name, steps[0].Card, tc.execute)
		})
	}
}

func sliceHas(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}
