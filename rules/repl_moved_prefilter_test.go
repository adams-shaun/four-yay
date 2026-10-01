package rules

import "testing"

// TestSpecSelfOnly pins the prefilter's spec classification: only a spec with
// no alternatives whose property chain carries the bare Self property admits
// nothing but its own source.
func TestSpecSelfOnly(t *testing.T) {
	for spec, want := range map[string]bool{
		"Card.Self":                           true,
		"Card.Self+!wasCastFromYourHandByYou": true,
		"Creature.YouCtrl+Self":               true,
		"Card.Self,Creature.Other":            false,
		"Card.!Self":                          false,
		"Card.Other":                          false,
		"Self":                                false,
		"Card.SelfOwn":                        false,
		"Creature.nonToken+YouCtrl":           false,
	} {
		if got := specSelfOnly(spec); got != want {
			t.Errorf("specSelfOnly(%q) = %v, want %v", spec, got, want)
		}
	}
}
