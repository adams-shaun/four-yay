package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestClashPlacementOneCardAndReverseChoices pins two things the multi-card
// Marvo fixture cannot: (1) a clashing player whose library holds exactly ONE
// card still gets its own top/bottom election (a one-card library is not the
// empty-library skip -- either answer leaves the order unchanged, but the
// election must still be posed); and (2) the independent elections run in
// participant order with the owner's own answer applied to the owner's own
// library, here reversed (controller bottom, opponent top).
func TestClashPlacementOneCardAndReverseChoices(t *testing.T) {
	e := combatEngine(t)
	marvo := onBoardCard(t, e, 0, unblockedCorpusCard(t, "m/marvo_deep_operative.txt"))
	e.G.Obj(marvo).SummonSick = false

	// Seat 0: a two-card library so BOTTOM is observable.
	for _, id := range e.G.Zone(state.ZLibrary, 0) {
		e.G.Obj(id).Zone = state.ZExile
	}
	e.G.SetZone(state.ZLibrary, 0, nil)
	filler := putTopOfLibrary(t, e, card(t, "Name:Controller filler\nManaCost:1\nTypes:Creature Beast\nPT:1/1\nOracle:x\n"), 0)
	high := putTopOfLibrary(t, e, card(t, "Name:Controller clash\nManaCost:5\nTypes:Creature Beast\nPT:5/5\nOracle:x\n"), 0)
	// Seat 1: exactly one card, so it is the whole library.
	for _, id := range e.G.Zone(state.ZLibrary, 1) {
		e.G.Obj(id).Zone = state.ZExile
	}
	e.G.SetZone(state.ZLibrary, 1, nil)
	low := putTopOfLibrary(t, e, card(t, "Name:Opponent clash\nManaCost:0\nTypes:Creature Beast\nPT:1/1\nOracle:x\n"), 1)

	// Preconditions: the comparison really favours seat 0, seat 0 has two
	// distinguishable cards, and seat 1's library is exactly one card.
	if e.G.Obj(high).Face().ManaValue() <= e.G.Obj(low).Face().ManaValue() {
		t.Fatalf("comparison precondition: high MV=%d, low MV=%d", e.G.Obj(high).Face().ManaValue(), e.G.Obj(low).Face().ManaValue())
	}
	if got := e.G.Zone(state.ZLibrary, 0); len(got) != 2 || got[0] != high || got[1] != filler {
		t.Fatalf("controller library precondition: %v", got)
	}
	if got := e.G.Zone(state.ZLibrary, 1); len(got) != 1 || got[0] != low {
		t.Fatalf("one-card opponent library precondition: %v", got)
	}

	e.emit(events.Event{Kind: events.DeclareAttackers, Player: 1, IDs: []state.ObjID{marvo}})
	e.putTriggersOnStack()
	e.resolveTop()

	for i, want := range []struct {
		player state.PlayerID
		choice string
	}{{0, "bottom"}, {1, "top"}} {
		d := e.Pending()
		if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "clash_placement" || d.Player != want.player || len(d.Options) != 2 {
			t.Fatalf("placement %d: expected player %d's election, got %+v", i, want.player, d)
		}
		selected := -1
		for _, option := range d.Options {
			if option.Kind == want.choice {
				selected = option.Index
			}
		}
		if selected < 0 {
			t.Fatalf("placement options = %+v, missing %s", d.Options, want.choice)
		}
		submitChoices(t, e, selected)
	}

	// Seat 0 chose BOTTOM: its revealed card moves under the filler.
	if got := e.G.Zone(state.ZLibrary, 0); len(got) != 2 || got[0] != filler || got[1] != high {
		t.Fatalf("controller bottom election = %v, want [%d %d]", got, filler, high)
	}
	// Seat 1 chose TOP on its only card: the order cannot change, but the
	// election was still posed (asserted above) and no reorder is emitted.
	if got := e.G.Zone(state.ZLibrary, 1); len(got) != 1 || got[0] != low {
		t.Fatalf("one-card top election changed order: %v", got)
	}
	if hasNote(e, "unimplemented API Clash") {
		t.Fatal("Clash handler was not registered")
	}
}
