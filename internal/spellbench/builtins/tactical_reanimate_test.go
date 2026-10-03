package builtins

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"

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

// TestReanimateTargetsLotlethGiant: Dread Return's target is the creature
// whose return is worth most, ETB included. Every graveyard target used to
// score 0, so the lowest option index (here Generous Ent) won and the Spy
// combo milled its own library without the Giant's lethal trigger.
func TestReanimateTargetsLotlethGiant(t *testing.T) {
	f := newTacticalFixture(t)
	dread := f.hand("Dread Return")
	var grave []view.CardView
	for _, n := range []string{"Generous Ent", "Balustrade Spy", "Lotleth Giant", "Saruli Caretaker", "Wall of Roots", "Masked Vandal", "Sagu Wildling"} {
		grave = append(grave, f.card(n, 0))
	}
	f.v.Players[0].Graveyard = grave
	var opts []decision.Option
	for i, cv := range grave[:3] {
		opts = append(opts, decision.Option{Index: i, Kind: "card", Obj: cv.ID, Player: 0, Controller: 0})
	}
	d := decision.Decision{Seq: 3, Player: 0, Kind: decision.KTarget, Min: 1, Max: 1, Source: dread.ID,
		TargetEffect: &decision.TargetEffect{API: "ChangeZone"}, Options: opts}
	if o := f.chose(decide(t, f.seat(), f.v, d), d); o.Obj != grave[2].ID {
		t.Fatalf("Dread Return aimed at %d, want Lotleth Giant %d", o.Obj, grave[2].ID)
	}
}
