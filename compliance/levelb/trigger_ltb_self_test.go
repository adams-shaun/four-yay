package levelb_test

import (
	"testing"

	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// TestSelfLTBRealCards classifies corpus cards whose own trigger fires when
// they leave the battlefield for somewhere other than the graveyard alone.
func TestSelfLTBRealCards(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, tc := range []struct{ name, key, sub, gap string }{
		{"Featherbrained Filcher", "trigger#0.0", "trigger.ltb-self", ""},
		{"Mouser Foundry", "trigger#0.1", "trigger.ltb-self", ""},
		{"Three Tree Scribe", "trigger#0.0", "trigger.ltb-self", ""},
		{"Syr Vondam, Sunstar Exemplar", "trigger#0.1", "trigger.ltb-self", ""},
		{"Planetarium of Wan Shi Tong", "trigger#0.2", "trigger.gap:ChangesZone", "static trigger"},
		{"Superior Foes of Spider-Man", "trigger#0.1", "trigger.gap:ChangesZone", "static trigger"},
	} {
		t.Run(tc.name+" "+tc.key, func(t *testing.T) {
			c, ok := reg.Lookup(tc.name)
			if !ok {
				t.Fatalf("precondition: %s is not in the corpus", tc.name)
			}
			for _, r := range levelb.Requirements(c) {
				if r.Key != tc.key {
					continue
				}
				if r.Sub != tc.sub || r.Gap != tc.gap {
					t.Fatalf("%s %s: sub=%q gap=%q, want sub=%q gap=%q", tc.name, tc.key, r.Sub, r.Gap, tc.sub, tc.gap)
				}
				return
			}
			t.Fatalf("precondition: %s carries no requirement %s", tc.name, tc.key)
		})
	}
}
