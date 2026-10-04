package rules

// Kernel-era restorations of the namecard_test.go behaviour tests the W3
// legacy removal deleted: the same scenarios, driven through the resolution
// kernel (rules/resolve) instead of the suspend/resume protocol.

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// TestCabalTherapyNamesANonlandMidResolution pins the mid-resolution
// effNameCard path on the real corpus card: Cabal Therapy's `SP$ NameCard`
// resolves on the stack, where it must ASK its controller for a nonland name
// (the SA's ValidCards$ Card.nonLand), not silently name the top of the
// caster's own library.
func TestCabalTherapyNamesANonlandMidResolution(t *testing.T) {
	t.Parallel()
	e, _ := nameCardEngine(t, "Cabal Therapy")
	therapy := e.G.Zone(state.ZHand, 0)[0]
	e.G.Players[0].Pool[state.MB] = 1
	e.beginCast(0, decision.Option{Kind: "cast", Obj: therapy})
	if z := e.G.Obj(therapy).Zone; z != state.ZStack {
		t.Fatalf("Cabal Therapy zone after cast = %s, want stack", z)
	}
	kr6ResolveTop(e)

	d := pendingNameAsk(t, e, "Cabal Therapy resolution")
	if len(d.Options) < 1000 {
		t.Fatalf("Therapy offered only %d names; the full corpus universe is not wired", len(d.Options))
	}
	if labelIndex(d, "Wasteland") >= 0 || labelIndex(d, "Forest") >= 0 {
		t.Fatal("Cabal Therapy (Card.nonLand) offered a land name")
	}
	idx := labelIndex(d, "Grizzly Bears")
	if idx < 0 {
		t.Fatal("Cabal Therapy did not offer the unseen nonland Grizzly Bears")
	}
	submitChoices(t, e, idx)
	if got := e.G.Obj(therapy).ChosenName; got != "Grizzly Bears" {
		t.Fatalf("Cabal Therapy ChosenName = %q, want Grizzly Bears", got)
	}
	// The chained DBDiscard sub-ability still runs after the answer, asking
	// its own player target (the resolution was not wedged by the name ask).
	d = e.Pending()
	if d == nil || len(d.Options) == 0 || d.Options[0].Kind != "player" {
		t.Fatalf("Cabal Therapy did not continue to its Discard target ask: %+v", d)
	}
}
