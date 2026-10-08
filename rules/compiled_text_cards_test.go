package rules

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// TestCardTextMatchesFreshWalk: the slot-cached per-card contribution is the
// fresh walk's, and a card whose face lists change is recomputed.
func TestCardTextMatchesFreshWalk(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	n := 0
	for _, c := range reg.AllCards() {
		if n >= 3000 {
			break
		}
		got, want := cardTextOf(c), buildCardText(c)
		if !slices.Equal(got.texts, want.texts) || !slices.Equal(got.costs, want.costs) || len(got.sas) != len(want.sas) {
			t.Fatalf("%s: cached contribution differs from a fresh walk", c.Path)
		}
		if again := cardTextOf(c); again != got {
			t.Fatalf("%s: second lookup recomputed", c.Path)
		}
		n++
	}
	f := &cards.Face{Name: "X", ManaCost: "1 U"}
	c := &cards.Card{Faces: []*cards.Face{f}}
	c.Link()
	before := cardTextOf(c)
	f.ManaCost = "2 U"
	if after := cardTextOf(c); after == before || !slices.Contains(after.costs, "2 U") {
		t.Fatal("a changed face must not read the stale contribution")
	}
}
