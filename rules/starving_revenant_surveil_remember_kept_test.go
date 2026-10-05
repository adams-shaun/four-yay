package rules

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestStarvingRevenantSurveilRememberKeptAnswered drives the real ETB trigger
// through its answered Surveil KArrange. The kept card is event-backed memory,
// which the chained Remembered$Amount draw/life-loss legs then consume.
func TestStarvingRevenantSurveilRememberKeptAnswered(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	revenant := lookup(t, reg, "Starving Revenant")
	e, cfg := corpusEngineCfg(t, reg, []*cards.Card{revenant}, nil)
	src := moveCorpusCard(t, e, "Starving Revenant", 0, state.ZBattlefield)
	if o := e.G.Obj(src); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Starving Revenant source %d is not on battlefield: %+v", src, o)
	}
	// The helper drained only the entry event; the Revenant's ETB trigger is
	// now on the stack. Await its actual Surveil ask.
	d := passUntilResolved(t, e, 30)
	if d == nil || d.Kind != decision.KArrange || d.ResumeKind != "arrange" {
		t.Fatalf("ETB Surveil did not pose its answered KArrange: %+v", d)
	}
	if d.Player != 0 || d.Min != 0 || len(d.Options) != 2 {
		t.Fatalf("Surveil KArrange = %+v, want seat 0, Min 0, two looked-at cards", d)
	}
	library := e.G.Zone(state.ZLibrary, 0)
	if len(library) < 2 {
		t.Fatalf("precondition: library has %d cards, want the two looked-at cards", len(library))
	}
	options := []state.ObjID{d.Options[0].Obj, d.Options[1].Obj}
	if options[0] == options[1] {
		t.Fatalf("precondition: offered options are not distinct: %v", options)
	}
	for _, id := range options {
		if !slices.Contains(library, id) {
			t.Fatalf("precondition: offered card %d is not in the library %v", id, library)
		}
	}
	kept := options[0]
	buried := options[1]
	if kept == buried {
		t.Fatalf("precondition: kept and graveyard card are equal: %d", kept)
	}
	lifeBefore := e.G.Players[0].Life
	drawsBefore := countEvents(e, func(ev events.Event) bool { return ev.Kind == events.Draw && ev.Player == 0 })

	// Keeping only option 0 puts it on top; the other looked-at card goes to
	// the graveyard. This deliberately differs from the no-host stand-in.
	submitChoices(t, e, d.Options[0].Index)
	var memoryEvent *events.Event
	for i := range e.L.Events {
		ev := &e.L.Events[i]
		if ev.Kind == events.Choose && ev.Obj == src && ev.Counter == "remembered" {
			memoryEvent = ev
		}
	}
	if memoryEvent == nil {
		t.Fatal("answered Surveil emitted no event-backed remembered Choose")
	}
	if len(memoryEvent.IDs) != 1 || memoryEvent.IDs[0] != kept {
		t.Fatalf("remembered Choose IDs = %v, want exactly kept card %d (not graveyard card %d)", memoryEvent.IDs, kept, buried)
	}
	if got := e.G.Obj(buried); got == nil || got.Zone != state.ZGraveyard {
		t.Fatalf("non-kept card %d is not in graveyard: %+v", buried, got)
	}
	var arrangedTop state.ObjID
	for _, ev := range e.L.Events {
		if ev.Kind == events.LibraryOrder && ev.Player == 0 && len(ev.IDs) > 0 {
			arrangedTop = ev.IDs[0]
		}
	}
	if arrangedTop != kept {
		t.Fatalf("answered Surveil top card = %d, want kept card %d", arrangedTop, kept)
	}

	passUntilStackEmpty(t, e, 30)
	if got := e.G.Obj(kept); got == nil || got.Zone != state.ZHand {
		t.Fatalf("remembered top card %d was not the rider's draw: %+v", kept, got)
	}
	drawsAfter := countEvents(e, func(ev events.Event) bool { return ev.Kind == events.Draw && ev.Player == 0 })
	if got := e.G.Players[0].Life; got != lifeBefore-3 {
		t.Fatalf("Revenant life = %d after keeping one, want %d (lose 3)", got, lifeBefore-3)
	}
	if drawsAfter-drawsBefore != 1 {
		t.Fatalf("Revenant drew %d cards after keeping one, want exactly 1", drawsAfter-drawsBefore)
	}
	replayCheck(t, e, cfg)
}
