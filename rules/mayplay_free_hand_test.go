package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// A MayPlayWithoutManaCost$ True static over AffectedZone$ Hand
// (Omniscience, Fires of Invention, Dracogenesis) is delivered as a free
// alternative cost on the ordinary hand cast (CR 118.9). Before it was, the
// may-play family read only graveyard/exile/library cards, so Omniscience
// never offered a free cast at all.

func freeHandCastOption(e *Engine, p state.PlayerID, id state.ObjID) (decision.Option, bool) {
	for _, o := range e.legalActions(p) {
		if o.Kind == "cast" && o.Obj == id && o.AltCostIndex > 0 {
			return o, true
		}
	}
	return decision.Option{}, false
}

// TestOmniscienceCastsFromHandWithoutMana: with Omniscience on the
// battlefield and no mana at all, Craw Wurm is offered as a free cast and
// reaches the stack.
func TestOmniscienceCastsFromHandWithoutMana(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusAlternativeCard(t, "Omniscience"), corpusAlternativeCard(t, "Craw Wurm"))
	omni, wurm := e.G.Zone(state.ZHand, 0)[0], e.G.Zone(state.ZHand, 0)[1]
	e.emit(events.Event{Kind: events.MoveZone, Obj: omni, From: state.ZHand, To: state.ZBattlefield})
	opt, ok := freeHandCastOption(e, 0, wurm)
	if !ok {
		t.Fatalf("no free cast of Craw Wurm offered under Omniscience: %+v", e.legalActions(0))
	}
	e.beginCast(0, opt)
	if got := e.G.Obj(wurm).Zone; got != state.ZStack {
		t.Fatalf("Craw Wurm after the free cast = %s, want stack", got)
	}
	e.resolveTop()
	if got := e.G.Obj(wurm).Zone; got != state.ZBattlefield {
		t.Fatalf("Craw Wurm after resolving = %s, want battlefield", got)
	}
}

// TestFreeHandCastIsTheStaticControllersAlone: Fires of Invention's
// Affected$ carries no YouOwn qualifier, but the permission is its
// controller's alone -- an opponent's hand card is not offered free.
func TestFreeHandCastIsTheStaticControllersAlone(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusAlternativeCard(t, "Fires of Invention"))
	fires := e.G.Zone(state.ZHand, 0)[0]
	e.emit(events.Event{Kind: events.MoveZone, Obj: fires, From: state.ZHand, To: state.ZBattlefield})
	// Seat 0 controls one land, so mana value 1 qualifies.
	land := e.G.AddObject(card(t, "Name:Land\nTypes:Basic Land Mountain\nOracle:x\n"), 0)
	land.Zone = state.ZBattlefield
	e.G.SetZone(state.ZBattlefield, 0, append(e.G.Zone(state.ZBattlefield, 0), land.ID))
	mine := e.G.AddObject(corpusAlternativeCard(t, "Lightning Bolt"), 0)
	mine.Zone = state.ZHand
	e.G.SetZone(state.ZHand, 0, append(e.G.Zone(state.ZHand, 0), mine.ID))
	theirs := e.G.AddObject(corpusAlternativeCard(t, "Lightning Bolt"), 1)
	theirs.Zone = state.ZHand
	e.G.SetZone(state.ZHand, 1, []state.ObjID{theirs.ID})
	if _, ok := freeHandCastOption(e, 0, mine.ID); !ok {
		t.Fatal("Fires of Invention's controller is not offered the free Lightning Bolt")
	}
	e.G.Priority = 1
	if _, ok := freeHandCastOption(e, 1, theirs.ID); ok {
		t.Fatal("the opponent was offered a free cast through Fires of Invention")
	}
}
