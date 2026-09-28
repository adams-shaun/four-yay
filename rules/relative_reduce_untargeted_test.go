package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestRelativeReduceCostWithoutTargetsStillApplies pins the target-independent
// half of the Relative$ ReduceCost gate: Hollow One's self reduction
// (Amount$ Y over PlayerCountPropertyYou$CardsDiscardedThisTurn/Twice) names
// no targets, so a cast that announces none must still price it. Gating the
// Relative$ exception on a non-empty target list priced it at {5} after a
// discard instead of {3}.
func TestRelativeReduceCostWithoutTargetsStillApplies(t *testing.T) {
	t.Parallel()
	filler := card(t, "Name:Test Filler\nManaCost:1\nTypes:Sorcery\nA:SP$ Draw | NumCards$ 1\nOracle:x\n")
	e := handEngine(t, corpusAlternativeCard(t, "Hollow One"), filler)
	hand := e.G.Zone(state.ZHand, 0)
	var spell, discard state.ObjID
	for _, id := range hand {
		if e.G.Obj(id).Face().Name == "Hollow One" {
			spell = id
		} else {
			discard = id
		}
	}
	if spell == 0 || discard == 0 {
		t.Fatalf("precondition: hand %v lacks Hollow One or the filler", hand)
	}
	e.emit(events.Discard(discard, 0))
	if got := e.CardsDiscardedThisTurn(0); got != 1 {
		t.Fatalf("precondition: CardsDiscardedThisTurn(0) = %d, want 1", got)
	}
	base := e.parseCost("5")
	statics := e.collectCostStatics()
	mods := e.costModifiersWithTargetsUsing(statics, 0, spell, spellScope(""), nil, false)
	if got := mods.apply(base).Generic; got != 3 {
		t.Fatalf("Hollow One after one discard costs {%d}, want {3}", got)
	}
}
