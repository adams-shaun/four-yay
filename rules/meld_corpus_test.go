package rules

import (
	"sort"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// Measured at the pinned Forge corpus: exactly seven card scripts contain an
// AB$/DB$ Meld instruction. Pin card names rather than script paths so this
// census runs over the same compiled registry that a match actually uses.
var meldCarriers = []string{
	"Gisela, the Broken Blade",
	"Graf Rats",
	"Hanweir Battlements",
	"Mishra, Claimed by Gix",
	"Titania, Voice of Gaea",
	"Urza, Lord Protector",
	"Vanille, Cheerful l'Cie",
}

func TestMeldCorpusCarriers(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	found := make(map[string]bool)
	for _, c := range reg.Cards {
		if c == nil || len(c.Faces) == 0 || c.Faces[0] == nil {
			continue
		}
		front := c.Faces[0].Name
		for _, f := range c.Faces {
			if f == nil {
				continue
			}
			inspect := func(sa *cards.SA) {
				for ; sa != nil; sa = sa.Sub {
					if sa.API != "Meld" || (sa.Kind != "AB" && sa.Kind != "DB") {
						continue
					}
					found[front] = true
					p := effects.MeldOf(sa)
					if p == nil {
						t.Fatalf("%s: meld instruction has no compiled parameters", front)
					}
					if p.Primary != front || p.Secondary == "" || p.Name == "" ||
						len(c.Faces) != 2 || c.Faces[1] == nil || c.Faces[1].Name != p.Name {
						t.Errorf("%s: meld instruction not bound to its primary/secondary/result faces: %+v", front, p)
					}
					if facts := effects.NewSAFacts(sa); facts.Meld == nil || facts.Meld.Primary != front {
						t.Errorf("%s: Meld missing from configured ability facts", front)
					}
					// These differing carriers prove the optional modifiers were
					// compiled as values, not simply defaulted for every Meld.
					switch front {
					case "Mishra, Claimed by Gix":
						if !p.Tapped || !p.Attacking {
							t.Errorf("Mishra: tapped/attacking modifiers lost: %+v", p)
						}
					case "Titania, Voice of Gaea":
						if p.SecondaryType != "Land" || p.Tapped || p.Attacking {
							t.Errorf("Titania: secondary type or defaults lost: %+v", p)
						}
					case "Vanille, Cheerful l'Cie":
						if p.Tapped || p.Attacking || p.SecondaryType != "" {
							t.Errorf("Vanille: unexpected meld modifiers: %+v", p)
						}
					}
				}
			}
			for _, a := range f.Abilities {
				inspect(a)
			}
			for _, tr := range f.Triggers {
				inspect(tr.Effect)
			}
			for _, r := range f.Repls {
				inspect(r.With)
			}
			f.EachSVarAbility(inspect)
		}
	}
	got := make([]string, 0, len(found))
	for name := range found {
		got = append(got, name)
	}
	sort.Strings(got)
	if len(got) != 7 {
		t.Errorf("compiled Meld carrier count = %d, want 7", len(got))
	}
	if len(got) != len(meldCarriers) {
		t.Errorf("compiled Meld carriers = %q; want %q", got, meldCarriers)
		return
	}
	for i := range got {
		if got[i] != meldCarriers[i] {
			t.Errorf("compiled Meld carrier %d = %q; want %q", i, got[i], meldCarriers[i])
		}
	}
}
