package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/effects"
)

// implementedStats are the four Mode$ statics that are genuinely read
// engine-side but were missing from rules/statics.go's stat
// effects.RegisterNonAPI call, so the coverage census counted every printed
// carrier as unsupported. Each name is paired with the read site that proves
// the read is real.
var implementedStats = []struct {
	primitive string
	readSite  string
}{
	{"stat:TapPowerValue", "Engine.tapPowerValue (rules/statics.go)"},
	{"stat:CantExile", "Engine.exileBlocked (rules/layers.go)"},
	{"stat:WitherDamage", "Engine.witherDamageStaticActive (rules/wither.go)"},
	{"stat:Activations", "Engine.additionalActivationLimit (rules/legal.go)"},
}

// TestImplementedStatsAreRegistered pins the coverage half of the fix: each
// implemented-but-previously-unregistered static must be in the engine's
// supported set, or the coverage walk still reports every printed carrier as
// unsupported -- the ticket's headline symptom. The engine genuinely reads
// each mode through activeStatics (proved by the existing per-primitive proof
// tests named in rules/statics.go's registration comment), so the
// registration is honest.
func TestImplementedStatsAreRegistered(t *testing.T) {
	t.Parallel()
	supported := effects.Supported()
	for _, s := range implementedStats {
		if !supported[s.primitive] {
			t.Errorf("effects.Supported() is missing %q even though it is read by %s",
				s.primitive, s.readSite)
		}
	}
}

// TestImplementedStatsCensusCarriersAreNotBlocked is the census half: no
// printed carrier of any of the four statics may be unsupported BECAUSE of
// that primitive. A carrier may still be unsupported for an unrelated
// primitive (Dragonfly Pilot also needs api:MakeCard, Experimental Pilot also
// needs api:Draft), which is correct and must not be asserted away. The
// precondition per primitive asserts the census actually found carriers, so a
// green result cannot be a vacuous scan of an empty registry.
func TestImplementedStatsCensusCarriersAreNotBlocked(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	supported := effects.Supported()
	for _, s := range implementedStats {
		carriers := 0
		var blocked []string
		for _, c := range reg.Cards {
			has := false
			for _, p := range c.Primitives() {
				if p == s.primitive {
					has = true
					break
				}
			}
			if !has {
				continue
			}
			carriers++
			for _, m := range reg.Unsupported(c, supported) {
				if m == s.primitive {
					blocked = append(blocked, c.Faces[0].Name)
				}
			}
		}
		if carriers == 0 {
			t.Errorf("precondition: no corpus carrier of %s found", s.primitive)
			continue
		}
		if len(blocked) != 0 {
			t.Errorf("%s carriers still blocked by the primitive (%d of %d): %v",
				s.primitive, len(blocked), carriers, blocked)
		}
	}
}
