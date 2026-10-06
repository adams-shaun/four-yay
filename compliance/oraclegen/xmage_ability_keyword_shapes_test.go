package oraclegen_test

import (
	"testing"

	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/internal/testutil"
)

func TestXMageAbilityKeywordShapes(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, tc := range []struct {
		card string
		want map[int]string
	}{
		{"Hobbit Hole", map[int]string{0: "{T}, Sacrifice {this}", 1: "Halflingcycling {4}"}},
		{"Cool but Rude", map[int]string{0: "{1}{R}: Level 2", 1: "{1}{R}: Level 3"}},
		{"Leader's Talent", map[int]string{0: "{2}{W}: Level 2", 1: "{3}{W}: Level 3"}},
		{"Bard's Bow", map[int]string{0: "Perseus's Bow — Equip {6}"}},
		{"Dragoon's Lance", map[int]string{0: "Gae Bolg — Equip {4}"}},
		{"Cori-Steel Cutter", map[int]string{0: "Equip {1}{R}"}},
		{"Caduceus Staff of Hermes", map[int]string{0: "Equip {W}{W}"}},
		{"A.I.M. Scientists", map[int]string{0: "Basic landcycling {2}"}},
		{"Silver-Fur Master", map[int]string{0: "Ninjutsu {U}{B}"}},
	} {
		t.Run(tc.card, func(t *testing.T) {
			c, ok := reg.Lookup(tc.card)
			if !ok || len(c.Faces) == 0 {
				t.Fatalf("precondition: %s missing from corpus", tc.card)
			}
			got, why := oraclegen.XMageAbility(c.Faces[0])
			if why != "" {
				t.Fatalf("mapping ambiguous: %s", why)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("mapped %v, want %v", got, tc.want)
			}
			for i, want := range tc.want {
				if got[i] != want {
					t.Errorf("ability %d prefix = %q, want %q", i, got[i], want)
				}
			}
		})
	}
}
