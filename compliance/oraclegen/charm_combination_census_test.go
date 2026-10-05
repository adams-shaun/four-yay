package oraclegen

import (
	"strconv"
	"testing"

	"github.com/adams-shaun/gorge/cards"
)

// TestCharmCombinationFixtureCensus keeps Choose-N charms distinct from the
// per-slot census: every reported carrier must have a fixture for a complete
// legal combination, not merely for each mode considered in isolation.
func TestCharmCombinationFixtureCensus(t *testing.T) {
	reg := censusRegistry(t)
	cases := []struct {
		name  string
		count int
	}{
		{"Ashling's Command", 2},
		{"Brigid's Command", 2},
		{"Grub's Command", 2},
		{"Sygg's Command", 2},
		{"Trystan's Command", 2},
		{"Return from the Wilds", 2},
		{"Cosmium Confluence", 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			card, ok := reg.Lookup(tc.name)
			if !ok || len(card.Faces) == 0 {
				t.Fatalf("card %q missing from corpus", tc.name)
			}
			face := card.Faces[0]
			var charm *cards.SA
			charmIndex := -1
			for i, ability := range face.Abilities {
				if ability.Kind == "SP" && ability.API == "Charm" {
					charm = ability
					charmIndex = i
					break
				}
			}
			if charm == nil || charm.Params["CharmNum"] != strconv.Itoa(tc.count) {
				t.Fatalf("loaded face does not have expected CharmNum$ %d", tc.count)
			}
			combos := CharmCombinations(face)
			if len(combos) == 0 {
				t.Fatal("no legal mode combinations")
			}
			foundFixture := false
			for _, combo := range combos {
				if len(combo.Modes) != tc.count {
					t.Fatalf("combination has %d modes, want %d", len(combo.Modes), tc.count)
				}
				if len(fixtures(reg, combo.Slots)) > 0 {
					foundFixture = true
				}
			}
			if !foundFixture {
				t.Fatalf("no complete combination fixture; independent modes are insufficient")
			}
			if ok, reason := FaceHasFixture(reg, face); !ok {
				t.Fatalf("FaceHasFixture rejected supported combination: %s", reason)
			}

			// The old census accepted any independently fixtureable mode,
			// even if the Charm demanded more picks than it offered.
			impossible := *face
			impossible.Abilities = append([]*cards.SA(nil), face.Abilities...)
			copyCharm := *charm
			copyCharm.Params = make(map[string]string, len(charm.Params))
			for key, value := range charm.Params {
				copyCharm.Params[key] = value
			}
			copyCharm.Params["CharmNum"] = strconv.Itoa(len(CharmModes(face)) + 1)
			copyCharm.Params["CanRepeatModes"] = "False"
			impossible.Abilities[charmIndex] = &copyCharm
			if ok, reason := FaceHasFixture(reg, &impossible); ok {
				t.Fatalf("census accepted a charm with no legal complete combination (reason %q)", reason)
			}
		})
	}
}
