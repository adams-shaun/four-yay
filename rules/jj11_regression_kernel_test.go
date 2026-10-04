package rules

// Kernel-era restorations of the jj11_regression_test.go behaviour tests the W3
// legacy removal deleted: the same scenarios, driven through the resolution
// kernel (rules/resolve) instead of the suspend/resume protocol.

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// Pin both the full permutation and the re-entry guard on a real chained SA.
// Ponder's optional shuffle is now real (task inbox-paramcensus-final-stragglers):
// the arrange answer is followed by the may-shuffle ask, which this oracle
// declines so the assertions below stay exactly the permutation/draw-once
// contract they always certified (the shuffle arm itself is pinned in
// rules/ponder_test.go).
func TestPonderArrangeResumesDrawExactlyOnce(t *testing.T) {
	e := crResolutionEngine(t, []string{"Ponder"}, nil)
	id := crAbortMove(t, e, 0, "Ponder", state.ZHand)
	sa := e.G.Obj(id).Face().SpellAbility()
	if sa == nil || sa.API != "RearrangeTopOfLibrary" || sa.Sub == nil || sa.Sub.API != "Draw" {
		t.Fatal("CR 608.2c: real Ponder arrange/draw chain missing")
	}
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "U", Amount: 1})
	e.askPriority(0)
	crAbortAnswer(t, e, "Ponder", crAbortOption(t, e, "Ponder", "cast", id))
	crResolutionRound(t, e)
	d := e.Pending()
	if d == nil || d.Kind != decision.KArrange || d.Min != 3 || d.Max != 3 || len(d.Options) != 3 {
		t.Fatalf("CR 608.2c: expected the full-permutation arrange ask, got %+v", d)
	}
	before := slices.Clone(e.G.Zone(state.ZLibrary, 0))
	if len(before) < 5 {
		t.Fatal("CR 608.2c: need untouched remainder")
	}
	for i, o := range d.Options {
		if o.Obj != before[i] {
			t.Fatal("CR 608.2c: top options do not describe live top")
		}
	}
	draws := countDraw(e)
	start := len(e.L.Events)
	crAbortAnswer(t, e, "Ponder", d.Options[2].Index, d.Options[0].Index, d.Options[1].Index)
	// The may-shuffle ask the arrange answer re-entry poses: decline, so the
	// oracle below sees exactly the arrangement + draw it always certified.
	sd := e.Pending()
	if sd == nil || sd.ResumeKind != "arrange_mayshuffle" {
		t.Fatalf("CR 608.2c: expected the may-shuffle ask, got %+v", sd)
	}
	noIdx := -1
	for _, o := range sd.Options {
		if o.Kind == "no" {
			noIdx = o.Index
		}
	}
	crAbortAnswer(t, e, "Ponder", noIdx)
	wantOrder := append([]state.ObjID{before[2], before[0], before[1]}, before[3:]...)
	orders := 0
	for _, ev := range e.L.Events[start:] {
		if ev.Kind == events.LibraryOrder {
			orders++
			if !slices.Equal(ev.IDs, wantOrder) {
				t.Fatalf("CR 608.2c: permutation=%v want %v", ev.IDs, wantOrder)
			}
		}
	}
	if orders != 1 || countDraw(e) != draws+1 || e.G.Obj(id).Zone != state.ZGraveyard || len(e.G.Stack) != 0 {
		t.Fatalf("CR 608.2c/608.2n: orders=%d draws=%d zone=%s stack=%v", orders, countDraw(e)-draws, e.G.Obj(id).Zone, e.G.Stack)
	}
	if !slices.Equal(e.G.Zone(state.ZLibrary, 0), wantOrder[1:]) {
		t.Fatal("CR 608.2c: draw did not consume chosen top, or remainder changed")
	}
	t.Log("real Ponder: full permutation applied, chained draw once, no re-ask, spell completed; the may-shuffle ask was declined to keep this oracle")
}
