package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestDamageAllReplaceDyingDefinedExilesInstead closes the printed clause of
// Anger of the Gods ("If a creature dealt damage this way would die this
// turn, exile it instead") end to end, through the real engine. effDamageAll
// damages the sweep and -- with RememberDamaged$ True -- remembers the hit
// creatures; registerReplaceDying then turns the state-based lethal-damage
// move into an exile. Before the fix effDamageAll never called
// registerReplaceDying, so the 2/2 went to the graveyard.
func TestDamageAllReplaceDyingDefinedExilesInstead(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	anger := mustCorpusCard(t, reg, "Anger of the Gods")
	bear := mustCorpusCard(t, reg, "Grizzly Bears")
	e, cfg := censusEngine(t, 9331, []*cards.Card{anger, bear}, nil)
	cardToHand(t, e, anger)
	bear0 := moveOwnerCard(t, e, 0, bear, state.ZBattlefield)
	addMana(t, e, 0, "GRR")
	castFirst(t, e, "cast")
	passUntilStackEmpty(t, e, 20)

	if o := e.G.Obj(bear0); o.Zone != state.ZExile {
		t.Fatalf("the dealt-damage bear should be exiled, not dying: %s", o.Zone)
	}
	if !movedToExile(e, bear0) {
		t.Fatal("no logged MoveZone into exile for the swept creature")
	}
	n := 0
	for _, ce := range e.continuous {
		if ce.ReplacementEvent == "Moved" {
			n++
			if len(ce.Remembered) != 1 || ce.Remembered[0] != bear0 {
				t.Fatalf("replacement remembered %v, want the bear %d", ce.Remembered, bear0)
			}
		}
	}
	if n != 1 {
		t.Fatalf("want exactly one Moved replacement registered, got %d", n)
	}
	replayCheck(t, e, cfg)
}
