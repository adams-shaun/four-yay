package builtins

import (
	"testing"

	"github.com/adams-shaun/gorge/internal/spellbench"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/view"
)

// TestReanimateValueTerminates: a creature whose own ETB returns a creature
// card from the graveyard to the battlefield, sitting in our graveyard, used
// to make reanimateValue recurse until the stack overflowed (found by
// sb-search's rollouts on the FDN catalog). Every such creature in the
// catalogs' decks is scored, and scoring returns.
func TestReanimateValueTerminates(t *testing.T) {
	f := newTacticalFixture(t)
	reg := testutil.CorpusRegistry(t)
	found := 0
	for _, cat := range spellbench.Catalogs {
		ids, err := spellbench.CatalogIDs(cat.Dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, id := range ids {
			deck, err := spellbench.Deck(reg, cat.Dir, id)
			if err != nil {
				t.Fatal(err)
			}
			for _, c := range deck {
				p := profileOf(c)
				self := false
				for _, e := range p.etb {
					self = self || (e.class == effRecursion && e.toBattlefield)
				}
				if !p.creature || !self {
					continue
				}
				found++
				f.v.Players[0].Graveyard = []view.CardView{f.card(c.Faces[0].Name, 0)}
				s := f.seat()
				st := s.tac.newState(&f.v, 0)
				if v := s.tac.reanimateValue(st); v <= 0 {
					t.Fatalf("%s: reanimate value %v", c.Faces[0].Name, v)
				}
			}
		}
	}
	if found == 0 {
		t.Skip("no self-reanimating ETB creature in the catalogs")
	}
}
