package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

// TestMetallicMimicChosenTypeOtherFilter pins Mimic's two-direction shape on
// the real card: choosing its own type (Shapeshifter) still leaves its OWN
// entry bare — the Other half of Creature.ChosenType+Other+YouCtrl — and
// choosing a real creature type (Bear) gives a LATER creature of that type
// its counter.
func TestMetallicMimicChosenTypeOtherFilter(t *testing.T) {
	t.Parallel()
	t.Run("own entry of the chosen type is excluded", func(t *testing.T) {
		e := handEngine(t, corpusAlternativeCard(t, "Metallic Mimic"))
		id := e.G.Zone(state.ZHand, 0)[0]
		e.G.Players[0].Pool[state.MC] = 2
		castMode(t, e, id, "")
		chooseETBType(t, e, "Shapeshifter")
		kr5FinishCast(t, e, id)
		if got := e.G.Obj(id).Counter("P1P1"); got != 0 {
			t.Fatalf("Mimic entered with %d P1P1, want 0 (each OTHER creature of the chosen type; Mimic is itself a Shapeshifter)", got)
		}
	})
	t.Run("later creature of the chosen type gets one", func(t *testing.T) {
		e := handEngine(t,
			corpusAlternativeCard(t, "Metallic Mimic"),
			corpusAlternativeCard(t, "Grizzly Bears"))
		mimic := e.G.Zone(state.ZHand, 0)[0]
		e.G.Players[0].Pool[state.MC] = 2
		castMode(t, e, mimic, "")
		chooseETBType(t, e, "Bear")
		kr5FinishCast(t, e, mimic)
		if got := e.G.Obj(mimic).Counter("P1P1"); got != 0 {
			t.Fatalf("Mimic entered with %d P1P1, want 0", got)
		}
		bear := e.G.Zone(state.ZHand, 0)[0]
		placeFromHand(t, e, bear)
		if got := e.G.Obj(bear).Counter("P1P1"); got != 1 {
			t.Fatalf("later Bear entered with %d P1P1, want 1 (chosen type Bear, Other, YouCtrl)", got)
		}
	})
}

// kr5FinishCast is finishCast after a kernel-answered as-enters ask: the
// answering Submit re-executed the resolution to its end, so a spell already
// off the stack has nothing left to resolve.
func kr5FinishCast(t *testing.T, e *Engine, id state.ObjID) {
	t.Helper()
	if o := e.G.Obj(id); o != nil && o.Zone != state.ZStack && e.Pending() == nil {
		return
	}
	finishCast(t, e, id)
}
