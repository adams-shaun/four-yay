package botpolicy

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func TestTargetSpareManaPreservesColouredInstant(t *testing.T) {
	r := testutil.CorpusRegistry(t)
	instant, ok := r.Lookup("Unsummon")
	if !ok || len(instant.Faces) == 0 {
		t.Fatal("corpus Unsummon missing")
	}
	cost := instant.Faces[0].ManaCost
	if CmcOf(cost) != 1 || colourPips(cost)[state.MU] != 1 {
		t.Fatalf("Unsummon cost %q is not a one-blue instant", cost)
	}
	blue := cards.ManaProduction{}
	blue.Colour[state.MU] = 1
	green := cards.ManaProduction{}
	green.Colour[state.MG] = 1
	b := Board{Cards: TableOf(map[state.ObjID]Card{
		1: {OnBattlefield: true, Basic: true, Produces: blue},
		2: {OnBattlefield: true, Basic: true, Produces: green},
		3: {Castable: true, InstantSpeed: true, ManaCost: cost, CMC: CmcOf(cost)},
	})}
	if !b.Cards.Get(1).OnBattlefield || !b.Cards.Get(2).OnBattlefield || !b.Cards.Get(3).Castable || b.Cards.Get(1).Produces == b.Cards.Get(2).Produces {
		t.Fatal("test needs distinct live blue and green sources and a castable instant")
	}
	if b.hasSpareMana() {
		t.Fatal("one blue plus green cannot guarantee the blue instant after spending a source")
	}
	b.Cards.Set(4, Card{OnBattlefield: true, Basic: true, Produces: blue})
	if !b.hasSpareMana() {
		t.Fatal("two blue basics plus green should preserve the instant after any one source is spent")
	}
	// A source with a conditional activation is not guaranteed by a printed
	// production summary; the basic-land flag is the conservative boundary.
	b.Cards.Set(4, Card{OnBattlefield: true, Produces: blue})
	if b.hasSpareMana() {
		t.Fatal("a nonbasic mana ability cannot be assumed freely activatable")
	}
}
