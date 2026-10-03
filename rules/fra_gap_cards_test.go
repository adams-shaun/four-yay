package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// TestRealityFractureGapCardsSupported pins the cards this change made
// playable: every corpus api:Empower carrier, the scry/surveil-this-turn
// readers and Karn, Argent Defender's DisableTriggers static report no
// unsupported primitive. Sanctum Lurker, the 35th Empower carrier, also
// needed stat:IgnorePlaneswalkerZeroLoyaltyRule; it is pinned by
// TestRealityFractureGap2CardsSupported (fra_gap2_census_test.go).
func TestRealityFractureGapCardsSupported(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	sup := effects.Supported()
	names := []string{
		"Academic Ascent", "Arcane Amphisbaena", "Avatar of Burgeoning Echoes", "Campus Crier",
		"Countersculpt", "Fatehold Charm", "Hexhaven Battalion", "Inspired Tethermage",
		"Jace, Reality Sculptor", "Keeper of the Quiet Hour", "Mindseeker Oculus", "No Admittance",
		"Overwrite the Multiverse", "Plan for All Outcomes", "Protege's Awakening", "Repurposed Enforcer",
		"Rewrite Regrets", "Solve for Disappointment", "Tam's Resistance", "Theorist's Proxy",
		"Theorist's Sanctum", "Violent Echoes", "Vraska's Final Mercy",
		"Way of the Cryomancer", "Way of the Deathbringer", "Way of the Healer", "Way of the Mentor",
		"Way of the Necromancer", "Way of the Paradox", "Way of the Pyromancer", "Way of the Warlord",
		"Way of the Wildspeaker", "Way of the Mind Sculptor", "Jace's Machinations",
		"Desperate Futurescribe", "Proctor of Potential", "Surveillance Phantasm", "Darkblade Agent",
		"Karn, Argent Defender",
	}
	for _, n := range names {
		c := lookup(t, reg, n)
		if u := reg.Unsupported(c, sup); len(u) > 0 {
			t.Errorf("%s: unsupported %v", n, u)
		}
	}
}
