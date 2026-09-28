package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// TestAbilityZoneMaskMatchesAbilityZoneOK pins buildManaSAFacts' one-read
// zone mask to abilityZoneOK bit for bit, over every ActivationZone$ shape
// abilityZoneOK distinguishes (absent, each named zone, padded, unknown).
func TestAbilityZoneMaskMatchesAbilityZoneOK(t *testing.T) {
	t.Parallel()
	shapes := []map[string]string{
		{},
		{"ActivationZone": "Battlefield"},
		{"ActivationZone": "Graveyard"},
		{"ActivationZone": " Hand "},
		{"ActivationZone": "Exile"},
		{"ActivationZone": "Stack"},
		{"ActivationZone": "Command"},
		{"ActivationZone": ""},
	}
	for _, params := range shapes {
		ab := &cards.SA{Params: params}
		mask := abilityZoneMask(ab)
		for z := 0; z < 32; z++ {
			if got, want := mask&(1<<z) != 0, abilityZoneOK(ab, state.Zone(z)); got != want {
				t.Errorf("%v zone %d: mask %v, abilityZoneOK %v", params, z, got, want)
			}
		}
	}
}
