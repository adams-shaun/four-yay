package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// CR 702.16b: protection stops a PERMANENT (or a player) from being
// targeted. The CR 608.2b resolution recheck applied protectedFrom to a
// target in ANY zone, while the announcement offer gated it on the
// battlefield, so a spell or a graveyard card with protection was offered as
// a target and then the targeting spell fizzled on resolution.

// pickTarget answers the pending target ask with the option naming id.
func pickTarget(t *testing.T, e *Engine, id state.ObjID) {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("no target ask pending: %+v", d)
	}
	for i, o := range d.Options {
		if o.Obj == id {
			submitChoices(t, e, i)
			return
		}
	}
	t.Fatalf("object %d is not offered as a target: %+v", id, d.Options)
}

// TestCounterspellCountersProgenitusSpell: Progenitus's protection from
// everything does not work on the stack, so Counterspell counters it, and
// its "put into a graveyard from anywhere" replacement shuffles it into its
// owner's library instead.
func TestCounterspellCountersProgenitusSpell(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusAlternativeCard(t, "Progenitus"))
	prog := e.G.Zone(state.ZHand, 0)[0]
	cs := e.G.AddObject(corpusAlternativeCard(t, "Counterspell"), 1)
	cs.Zone = state.ZHand
	e.G.SetZone(state.ZHand, 1, []state.ObjID{cs.ID})
	for _, r := range "WWUUBBRRGG" {
		e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: string(r), Amount: 1})
	}
	e.emit(events.Event{Kind: events.ManaAdd, Player: 1, Counter: "U", Amount: 2})
	castMode(t, e, prog, "")
	e.G.Priority = 1
	e.beginCast(1, decision.Option{Kind: "cast", Obj: cs.ID})
	pickTarget(t, e, prog)
	e.resolveTop()
	if got := e.G.Obj(cs.ID).Zone; got != state.ZGraveyard {
		t.Fatalf("Counterspell after resolving = %s, want graveyard", got)
	}
	for _, ev := range e.L.Events {
		if ev.Kind == events.MoveZone && ev.Obj == cs.ID && ev.Text == "fizzled: no legal targets remain" {
			t.Fatal("Counterspell fizzled: protection was applied to a spell on the stack")
		}
	}
	if got := e.G.Obj(prog).Zone; got != state.ZLibrary {
		t.Fatalf("countered Progenitus = %s, want library (shuffled in instead of the graveyard)", got)
	}
	if len(e.G.Stack) != 0 {
		t.Fatalf("stack = %v, want empty", e.G.Stack)
	}
}

// TestRaiseDeadReturnsProtectionFromBlackCard: a card in a graveyard is not
// a permanent, so White Knight's protection from black does not stop the
// black Raise Dead from returning it.
func TestRaiseDeadReturnsProtectionFromBlackCard(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusAlternativeCard(t, "Raise Dead"))
	spell := e.G.Zone(state.ZHand, 0)[0]
	knight := e.G.AddObject(corpusAlternativeCard(t, "White Knight"), 0)
	knight.Zone = state.ZGraveyard
	e.G.SetZone(state.ZGraveyard, 0, []state.ObjID{knight.ID})
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "B", Amount: 1})
	castMode(t, e, spell, "")
	pickTarget(t, e, knight.ID)
	e.resolveTop()
	if got := e.G.Obj(knight.ID).Zone; got != state.ZHand {
		t.Fatalf("White Knight after Raise Dead = %s, want hand", got)
	}
}
