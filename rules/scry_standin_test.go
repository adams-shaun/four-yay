package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The scry stand-in completion (task scrybottom). A scry whose KArrange is
// never answerable -- an empty library or ScryNum$ 0, where the ask's only
// legal answer is the empty one and effects.Ask never posts it, or a no-host
// run -- completes in effLookAndArrange's stand-in, not in handleArrange.
// The completed events.Scry record must exist there too (Amount 0: every
// looked-at card stayed where it was), or the once-per-instruction contract
// of a plain Mode$ Scry trigger breaks exactly when a scry looks at nothing,
// and the ToBottom$ True gate would have nothing to read either way.

// drainScryResolution resolves the spell under the stack, answering every
// ask on the way. A KArrange here is itself a failure: this helper is only
// used on scrys whose ask is never posted.
func drainScryResolution(t *testing.T, e *Engine, limit int) {
	t.Helper()
	for i := 0; i < limit; i++ {
		if e.G.Over || len(e.G.Stack) == 0 {
			return
		}
		d := e.Pending()
		if d == nil {
			e.Advance()
			continue
		}
		switch d.Kind {
		case decision.KPriority:
			submitPass(t, e)
		case decision.KTriggerOrder:
			choices := make([]int, 0, len(d.Options))
			for _, o := range d.Options {
				choices = append(choices, o.Index)
			}
			submitChoices(t, e, choices...)
		case decision.KChoose:
			if len(d.Options) == 0 {
				t.Fatalf("KChoose with no options while draining the scry: %+v", d)
			}
			submitChoices(t, e, d.Options[0].Index)
		default:
			t.Fatalf("unexpected %s decision while draining the scry: %+v", d.Kind, d)
		}
	}
	t.Fatal("the scry resolution never drained")
}

// emptyLibrary moves every card of seat p's library into its graveyard
// through real logged MoveZone events, the direct-setup moveSeeded harness.
func emptyLibrary(t *testing.T, e *Engine, p state.PlayerID) {
	t.Helper()
	for _, id := range append([]state.ObjID(nil), e.G.Zone(state.ZLibrary, p)...) {
		e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZLibrary, To: state.ZGraveyard, Player: p})
	}
	e.priorityRound()
	if n := len(e.G.Zone(state.ZLibrary, p)); n != 0 {
		t.Fatalf("setup: seat %d's library still holds %d card(s), want 0", p, n)
	}
}

// TestScryZeroLookRecordsZeroBottomCompletion pins the stand-in half of the
// completion contract: casting the Scry 3 spell with an EMPTY library poses
// no KArrange at all, and the completed instruction must still record its
// zero-card bottom pile -- exactly one events.Scry marker with Amount 0 --
// so Matoya, Archon Elder's plain "Whenever you scry" fires ONCE while the
// Temporal Anchor's ToBottom$ True gate stays closed on the zero pile.
func TestScryZeroLookRecordsZeroBottomCompletion(t *testing.T) {
	t.Parallel()
	e, _, id := scryFixture(t, 205)
	matoya := onBoardCard(t, e, 0, corpusCard(t, "Matoya, Archon Elder"))
	anchor := onBoardCard(t, e, 0, corpusCard(t, "The Temporal Anchor"))
	if o := e.G.Obj(matoya); o == nil || o.Zone != state.ZBattlefield || len(o.Face().Triggers) == 0 || o.Face().Triggers[0].Mode != "Scry" {
		t.Fatalf("setup: Matoya, Archon Elder's Mode$ Scry trigger is not on the battlefield: %+v", o)
	}
	if o := e.G.Obj(anchor); o == nil || o.Zone != state.ZBattlefield || len(o.Face().Triggers) < 2 || o.Face().Triggers[1].Params["ToBottom"] != "True" {
		t.Fatalf("setup: The Temporal Anchor's ToBottom$ True trigger is not on the battlefield: %+v", o)
	}
	if len(e.G.Zone(state.ZLibrary, 0)) == 0 {
		t.Fatal("setup: library already empty before the test emptied it")
	}
	if len(scryMarkers(e)) != 0 {
		t.Fatalf("setup: %d scry marker(s) already in the log", len(scryMarkers(e)))
	}
	emptyLibrary(t, e, 0)
	// Cast the scry spell directly (castFixture's drain expects the cast to
	// suspend, which a zero-look scry never does), then resolve it: no
	// KArrange may ever be posed -- the ask's only legal answer is the empty
	// one, so the stand-in completes the scry without one.
	d0 := e.Pending()
	castIdx := -1
	for _, o := range d0.Options {
		if o.Kind == "cast" && o.Obj == id {
			castIdx = o.Index
		}
	}
	if castIdx < 0 {
		t.Fatalf("no cast option for the scry spell %d: %+v", id, d0.Options)
	}
	submitChoices(t, e, castIdx)
	drainScryResolution(t, e, 60)

	marks := scryMarkers(e)
	if len(marks) != 1 {
		t.Fatalf("completed scry records = %+v, want exactly one marker for the stand-in-completed scry", marks)
	}
	if marks[0].Amount != 0 || marks[0].Player != 0 || marks[0].Obj != id {
		t.Fatalf("completed scry record = %+v, want Player 0, the spell %d as source, and a ZERO bottom pile", marks[0], id)
	}
	// The plain "whenever you scry" contract: once per instruction, even
	// when the instruction bottomed nothing (here: looked at nothing).
	if n := anchorTriggerPushes(e, matoya); n != 1 {
		t.Fatalf("Matoya, Archon Elder pushed %d triggers for the zero-look scry, want exactly 1", n)
	}
	// The ToBottom$ True gate on a zero pile: nothing fired, nothing exiled.
	if n := anchorTriggerPushes(e, anchor); n != 0 {
		t.Fatalf("The Temporal Anchor pushed %d triggers on a zero bottom pile, want 0", n)
	}
	if n := len(libraryExiled(e, 0)); n != 0 {
		t.Fatalf("exiled %v from the library on a zero-look scry, want nothing", libraryExiled(e, 0))
	}
	// Handler-ran observable: the stand-in emitted its LibraryOrder (and the
	// marker above beside it) rather than leaving the verb unimplemented.
	sawOrder := false
	for _, ev := range e.L.Events {
		if ev.Kind == events.LibraryOrder && ev.Player == 0 {
			sawOrder = true
		}
	}
	if !sawOrder {
		t.Fatal("no LibraryOrder for the stand-in-completed scry: the Scry handler never ran its stand-in")
	}
}

// TestScryOncePerInstructionEvenWithZeroBottomed pins the ordinary path's
// half of the same contract: answering a real KArrange with keep-all-on-top
// records a zero-card bottom pile, and a plain Mode$ Scry trigger still
// fires exactly once for that instruction.
func TestScryOncePerInstructionEvenWithZeroBottomed(t *testing.T) {
	t.Parallel()
	e, _, id := scryFixture(t, 206)
	matoya := onBoardCard(t, e, 0, corpusCard(t, "Matoya, Archon Elder"))
	if o := e.G.Obj(matoya); o == nil || o.Zone != state.ZBattlefield || len(o.Face().Triggers) == 0 || o.Face().Triggers[0].Mode != "Scry" {
		t.Fatalf("setup: Matoya, Archon Elder's Mode$ Scry trigger is not on the battlefield: %+v", o)
	}
	d := scryDecision(t, e, id)
	if len(d.Options) != 3 || len(e.G.Zone(state.ZLibrary, 0)) < 4 || d.Options[0].Obj == d.Options[1].Obj {
		t.Fatalf("precondition: need three distinct offered cards and a nonempty remainder: %+v", d)
	}
	// Keep every looked-at card on top: the answer bottoms ZERO cards.
	submitChoices(t, e, d.Options[0].Index, d.Options[1].Index, d.Options[2].Index)
	passUntilStackEmpty(t, e, 40)
	marks := scryMarkers(e)
	if len(marks) != 1 || marks[0].Amount != 0 {
		t.Fatalf("completed scry records = %+v, want one marker with a zero bottom pile", marks)
	}
	if n := anchorTriggerPushes(e, matoya); n != 1 {
		t.Fatalf("Matoya, Archon Elder pushed %d triggers for a bottom-less scry, want exactly 1 (once per instruction)", n)
	}
}
