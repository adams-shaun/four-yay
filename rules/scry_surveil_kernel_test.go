package rules

// Kernel-era restorations of scry_surveil_test.go's legacy-only leaves: the
// Scry arrange decision's shape, and the Ruling J5 mixed-destination degrade.

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// Resolving a Scry 3 poses a KArrange decision with Min 0, Max 3, one
// "bottom" option per top card in top-down order, for the library owner,
// with the spell still resolving on the stack (a posed tape resolution).
func TestKr8ScryPosesArrange(t *testing.T) {
	e, _, id := scryFixture(t, 201)
	d := scryDecision(t, e, id)
	if d.Min != 0 || d.Max != 3 {
		t.Fatalf("Min/Max = %d/%d, want 0/3 (Scry may keep any subset on top)", d.Min, d.Max)
	}
	if len(d.Options) != 3 {
		t.Fatalf("options = %d, want 3", len(d.Options))
	}
	if d.Player != 0 {
		t.Fatalf("decision player = %d, want 0 (the library owner)", d.Player)
	}
	for i, o := range d.Options {
		if o.Kind != "bottom" {
			t.Fatalf("option %d Kind = %q, want \"bottom\" (the unchosen go to the bottom)", i, o.Kind)
		}
	}
	lib := e.G.Zone(state.ZLibrary, 0)
	for i, o := range d.Options {
		if o.Obj != lib[i] {
			t.Fatalf("option %d Obj = %v, want the top card %v (top-down order)", i, o.Obj, lib[i])
		}
	}
	if !TapePosed(e) {
		t.Fatal("no tape resolution posed: the asking spell must still be resolving")
	}
	if len(e.G.Stack) == 0 || e.G.Obj(id).Zone != state.ZStack {
		t.Fatal("the spell left the stack while its arrange is posed")
	}
}

// Ruling J5: a KArrange whose options disagree on a destination is a
// programming error, so the handler emits a Note and applies the Options[0]
// destination. A posed tape decision is rebuilt when its answer re-runs the
// resolution, so the disagreeing copy is handed to the answer record (the
// handler every arrange answer goes through) directly.
func TestKr8ArrangeMixedKindDegradesWithNote(t *testing.T) {
	e, _, id := scryFixture(t, 205)
	d := scryDecision(t, e, id)
	top := []state.ObjID{d.Options[0].Obj, d.Options[1].Obj, d.Options[2].Obj}
	libBefore := append([]state.ObjID(nil), e.G.Zone(state.ZLibrary, 0)...)
	bad := *d
	bad.Options = append([]decision.Option(nil), d.Options...)
	bad.Options[1].Kind = "graveyard"

	arrangeAnswerRecord(e, &bad, decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{2, 0}}, d.ResumeSA)

	if !hasNote(e, "arrange options disagree on a destination") {
		t.Fatal("no Note \"arrange options disagree on a destination\" emitted for a mixed-Kind arrange")
	}
	libAfter := e.G.Zone(state.ZLibrary, 0)
	want := make([]state.ObjID, 0, len(libBefore))
	want = append(want, top[2], top[0])
	want = append(want, libBefore[3:]...)
	want = append(want, top[1])
	if !sameObjIDs(want, libAfter) {
		t.Fatalf("mixed-Kind arrange applied the wrong destination: library = %v, want %v (Options[0]=\"bottom\")", libAfter, want)
	}
}
