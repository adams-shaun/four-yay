package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestDenryKlinCopiesHisOwnKindsOntoTheEnteringCreature pins the live-Self
// source (the second rung the ladder does NOT need): Denry sits on the
// battlefield holding a +1/+1 counter (his entry pick) and a charge counter
// added after entry; a nontoken creature entering copies BOTH kinds. The
// entering creature's own pre-entry count of zero is the vacuity guard.
func TestDenryKlinCopiesHisOwnKindsOntoTheEnteringCreature(t *testing.T) {
	t.Parallel()
	denry := mustCorpusCardT(t, "Denry Klin, Editor in Chief")
	entering := card(t, "Name:Arriving Bear\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	e, cfg := tokenReplGame(t, 451, denry, entering)
	toMain1(t, e)
	denryID := state.ObjID(0)
	for _, id := range e.G.Zone(state.ZLibrary, 0) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == denry.Faces[0].Name {
			denryID = id
			break
		}
	}
	if denryID == 0 {
		t.Fatal("precondition: Denry is not in the seeded library")
	}
	e.pending = nil
	e.probe(func() {
		e.emit(events.Event{Kind: events.MoveZone, Obj: denryID, From: state.ZLibrary, To: state.ZBattlefield})
	})
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "counter_kind" || d.Min != 1 || d.Max != 1 || len(d.Options) != 3 {
		t.Fatalf("Denry entry choice = %+v, want one counter-kind KChoose", d)
	}
	want := -1
	for _, o := range d.Options {
		if o.Label == "First Strike" {
			want = o.Index
		}
	}
	if want < 0 {
		t.Fatalf("Denry choices = %+v, want First Strike", d.Options)
	}
	submitChoices(t, e, want)
	kr5Settle(e) // the turn loop after the entry: Denry's own entry trigger reaches the stack
	if got := e.G.Obj(denryID).Counter("First Strike"); got != 1 {
		t.Fatalf("Denry First Strike = %d, want 1", got)
	}
	if got := e.G.Obj(denryID).Counter("P1P1,First Strike,Vigilance"); got != 0 {
		t.Fatalf("composite counter = %d, want zero", got)
	}
	e.emit(events.Event{Kind: events.CounterChange, Obj: denryID, Counter: "CHARGE", Amount: 1})
	e.pending = nil
	if got := e.G.Obj(denryID).Counter("First Strike"); got != 1 || e.G.Obj(denryID).Counter("CHARGE") != 1 {
		t.Fatalf("precondition: Denry counters %d First Strike / %d CHARGE, want 1/1",
			got, e.G.Obj(denryID).Counter("CHARGE"))
	}

	enteringID := moveSeededCard(t, e, 0, entering, state.ZBattlefield)
	if got := e.G.Obj(enteringID).Counter("P1P1"); got != 0 || e.G.Obj(enteringID).Counter("CHARGE") != 0 {
		t.Fatalf("precondition: entering bear starts countered (%d/%d)",
			e.G.Obj(enteringID).Counter("P1P1"), e.G.Obj(enteringID).Counter("CHARGE"))
	}
	e.priorityRound()
	passUntilStackEmpty(t, e, 20)

	if got := e.G.Obj(enteringID).Counter("First Strike"); got == 0 {
		t.Fatalf("entering bear First Strike = %d, want copied individual kind", got)
	}
	if got := e.G.Obj(enteringID).Counter("CHARGE"); got == 0 {
		t.Fatalf("entering bear CHARGE = %d, want copied kind", got)
	}
	replayCheck(t, e, cfg)
}
