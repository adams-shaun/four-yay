package rules

// cantdraw_census_test.go — the corpus census for the CR 121.6 CantDraw
// static (task cantdraw1). The set-audit test
// (setaudit_ecl_test.go TestSetAudit_ecl_MornsongAria_CantDrawStopsDrawStep)
// proves the behaviour end to end; this file proves the registration and
// names the other corpus carriers, so a carrier that drifts (or a dropped
// effects.RegisterNonAPI entry) fails loudly rather than silently shrinking
// the class being claimed.

import (
	"testing"

	"github.com/adams-shaun/gorge/effects"
)

// cantDrawCarriers is every corpus card measured on 2026-09-28 to carry an
// `S:Mode$ CantDraw` line:
//
//	/usr/bin/grep -rl "Mode\$ CantDraw" .cards/cardsfolder | wc -l  ==  7
//
// The first four are total prohibitions ("players can't draw cards"); the
// last three carry `DrawLimit$ 1` ("can't draw more than one card each
// turn"), the per-turn count caps enforced by rules.drawForbidden.
var cantDrawCarriers = []string{
	"Mornsong Aria",
	"Maralen of the Mornsong",
	"Maralen of the Mornsong Avatar",
	"Omen Machine",
	"Leovold, Emissary of Trest",
	"Narset, Parter of Veils",
	"Spirit of the Labyrinth",
}

// TestCantDrawCensus pins the class: the primitive is registered in
// effects.Supported(), and each of the seven corpus carriers still carries
// the static. Four are total prohibitions and three carry the
// DrawLimit$ cap; both directions are asserted so neither a dropped
// registration nor a carrier that changed shape goes unnoticed.
func TestCantDrawCensus(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	if !effects.Supported()["stat:CantDraw"] {
		t.Fatal("effects.Supported() lacks stat:CantDraw")
	}
	prohibitions, limits := 0, 0
	for _, name := range cantDrawCarriers {
		c := searchCorpusCard(t, reg, name)
		found := false
		for _, f := range c.Faces {
			for _, st := range f.Statics {
				if st.Mode != "CantDraw" {
					continue
				}
				found = true
				if _, ok := st.Params["DrawLimit"]; ok {
					limits++
				} else {
					prohibitions++
				}
			}
		}
		if !found {
			t.Errorf("%s no longer carries a CantDraw static", name)
		}
	}
	if prohibitions != 4 || limits != 3 {
		t.Errorf("CantDraw census: %d total prohibitions + %d DrawLimit carriers, want 4 + 3", prohibitions, limits)
	}
}
