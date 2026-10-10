package effects

import "testing"

func TestSpecScopeOf(t *testing.T) {
	cases := []struct {
		spec string
		want SpecScope
	}{
		{"Creature.YouCtrl", SpecScopeYouCtrl},
		{"Creature.Other+YouCtrl", SpecScopeYouCtrl},
		{"Creature.OppCtrl", SpecScopeNotYouCtrl},
		{"Creature.!YouCtrl", SpecScopeNotYouCtrl},
		{"Creature.!OppCtrl", SpecScopeYouCtrl},
		{"Card.Self", SpecScopeSelf},
		{"Creature.EquippedBy", SpecScopeAttached},
		{"Creature.EnchantedBy", SpecScopeAttached},
		// An unmodelled conjunct only narrows the matches.
		{"Creature.YouCtrl+IsRemembered", SpecScopeYouCtrl},
		// Every alternative must carry the claim.
		{"Creature.YouCtrl,Artifact.YouCtrl", SpecScopeYouCtrl},
		{"Creature.YouCtrl,Artifact", 0},
		{"Creature.YouCtrl,Artifact.OppCtrl", 0},
		// No control term, an unknown base, a negated Self, an EACH spec and
		// the empty spec prove nothing.
		{"Creature", 0},
		{"Vehicle.YouCtrl", SpecScopeYouCtrl},
		{"Card.!Self", 0},
		{"CARDNAME", 0},
		{"EACH Creature.YouCtrl & Artifact.YouCtrl", 0},
		{"", 0},
	}
	for _, c := range cases {
		if got := SpecScopeOf(c.spec); got != c.want {
			t.Errorf("SpecScopeOf(%q) = %b, want %b", c.spec, got, c.want)
		}
	}
}
