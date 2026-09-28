package rules

// The self-exile completion guard (task agent-20260922T150140Z-702b52c6):
// a spell whose own resolution chain moves it off the stack (Forge's
// `Origin$ Stack | Destination$ Exile` sub-ability) must keep that
// destination. The shared `moveResolvedOffStack` tail used to emit an
// unconditional stack->resting-zone move after resolution, so the spell
// landed in the graveyard despite its own exile. These tests pin the direct
// (non-suspended) resolution path -- the one `moveResolvedOffStack` serves.
// Every real card here is compiled from the corpus; no Forge script text is
// committed.

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestBurningWishEmptySideboardKeepsSelfExile drives Burning Wish with an
// EMPTY sideboard: the outside-the-game origin offers nothing, so no search
// ask is posed and the spell resolves directly (the path
// `moveResolvedOffStack` serves, not the resumed-search path). Its own
// DBChange sub-ability exiles the spell; completion must not then emit a
// trailing stack->graveyard move. This is the direct-path sibling of
// TestChangeZoneWishFindsSideboard (which suspends on a real sideboard pick
// and so already took the guarded resumed path).
func TestBurningWishEmptySideboardKeepsSelfExile(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	// searchEngine leaves cfg.Sideboards empty: no outside-the-game card.
	e, cfg := searchEngine(t, reg, "Burning Wish")
	start := len(e.L.Events)
	addMana(t, e, 0, "CR")
	id := searchMoveByName(t, e, "Burning Wish", state.ZHand)
	// Precondition the whole assertion rests on: the wish starts in hand, so
	// the move to exile below is this cast's own doing.
	if o := e.G.Obj(id); o == nil || o.Zone != state.ZHand {
		t.Fatalf("Burning Wish setup = %+v, want in hand", o)
	}
	castFixtureNamed(t, e, "Burning Wish")
	// An empty sideboard poses no search ask, so the cast resolves straight
	// through; there is no decision to answer.
	passUntilStackEmpty(t, e, 20)

	// (a) the self-exile ran, (b) the final zone is exile, (c) no trailing
	// stack->graveyard completion move exists.
	exiled := false
	for _, ev := range e.L.Events[start:] {
		if ev.Kind == events.MoveZone && ev.Obj == id && ev.From == state.ZStack && ev.To == state.ZExile {
			exiled = true
		}
	}
	if !exiled {
		t.Fatal("the wish's SubAbility$ self-exile did not run")
	}
	if o := e.G.Obj(id); o == nil || o.Zone != state.ZExile {
		t.Fatalf("Burning Wish final zone = %+v, want exile", o)
	}
	for _, ev := range e.L.Events[start:] {
		if ev.Kind == events.MoveZone && ev.Obj == id && ev.From == state.ZStack && ev.To == state.ZGraveyard {
			t.Fatalf("Burning Wish had a trailing stack-to-graveyard completion move: %+v", ev)
		}
	}
	replayCheck(t, e, cfg)
}
