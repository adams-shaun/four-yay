package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestSetLifeSpellTargetsAPlayerBlessedWind is the engine-level proof of the
// `SP$ SetLife | ValidTgts$ Player` shape (11 of the 45 SetLife carriers are
// SP$/AB$ ability lines, which the `(DB|AP|A)$` regex the dispatch brief used
// does not see; task eoe-the-endstone-set-life). Blessed Wind is
// "{7}{W}{W} sorcery — Target player's life total becomes 20.": the spell
// must ask for a player target and set the TARGET's life to 20, not the
// caster's.
func TestSetLifeSpellTargetsAPlayerBlessedWind(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := corpusEngine(t, reg, []*cards.Card{lookup(t, reg, "Blessed Wind")}, nil)
	bw := searchMoveByName(t, e, "Blessed Wind", state.ZHand)
	addMana(t, e, 0, "WWCCCCCCC") // {7}{W}{W}

	// Precondition: the target's life differs from both 20 and the
	// caster's, so a leaked-to-controller bug and a no-op are distinguishable.
	e.G.Players[0].Life = 9
	e.G.Players[1].Life = 7
	if e.G.Players[1].Life == 20 || e.G.Players[0].Life == e.G.Players[1].Life {
		t.Fatal("precondition: target life must differ from 20 and from the caster's")
	}

	d := e.Pending()
	if d == nil {
		t.Fatal("no priority decision before casting Blessed Wind")
	}
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == bw {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no cast option for Blessed Wind: %+v", d.Options)
	}
	submitChoices(t, e, idx)

	// The spell must offer a player target; choose the OPPONENT (seat 1).
	d = e.Pending()
	if d == nil || d.Kind != "target" {
		t.Fatalf("Blessed Wind posed no target decision: %+v", d)
	}
	chooseTargetPlayer(t, e, 1)
	passUntilStackEmpty(t, e, 40)

	if got := e.G.Players[1].Life; got != 20 {
		t.Fatalf("targeted player life = %d, want 20", got)
	}
	if got := e.G.Players[0].Life; got != 9 {
		t.Fatalf("caster life = %d, want 9 (the set leaked to the caster)", got)
	}
}
