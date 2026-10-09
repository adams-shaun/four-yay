package rules

// Toluz, Clever Conductor's discard batch trigger:
// "Whenever you discard one or more cards, exile them from your graveyard."
// Its body is `DB$ ChangeZoneAll | Origin$ Graveyard | Destination$ Exile |
// ChangeType$ Card.TriggeredCards` — the sweep must move exactly the discarded
// cards the trigger CAPTURED (Ctx.Remembered) from the graveyard to exile. This
// file is the engine-level carrier for the ChangeZoneAll ChangeType$
// Card.TriggeredCards referent with an Exile destination.

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

// TestToluzCleverConductorExilesDiscardedCard drives one real discard of a real
// corpus card and pins the leg: the discarded card reaches exile (the sweep
// moves the captured card out of the graveyard), and it is the captured card
// alone that moves.
func TestToluzCleverConductorExilesDiscardedCard(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	toluz := searchCorpusCard(t, reg, "Toluz, Clever Conductor")
	e, _, _ := discardedAllEngine(t, 9119, toluz)

	tID := onBoardCard(t, e, 0, toluz)
	if o := e.G.Obj(tID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("Toluz id %d zone = %v, want battlefield (vacuous setup)", tID, o)
	}

	// Seed one real corpus card into seat 0's hand, with the hand otherwise
	// empty so the discard batch holds exactly it.
	emptyHandToLibrary(t, e, 0)
	bears := mustCorpusCard(t, reg, "Grizzly Bears")
	fodder := e.G.AddObject(bears, 0)
	fodder.Zone = state.ZHand
	e.G.SetZone(state.ZHand, 0, []state.ObjID{fodder.ID})
	if o := e.G.Obj(fodder.ID); o == nil || o.Zone != state.ZHand || o.Owner != 0 {
		t.Fatalf("precondition failed: fodder zone=%v owner=%d, want hand/0", o.Zone, o.Owner)
	}

	// Resolve a real Mode$ Hand discard: the card leaves the hand for the
	// graveyard, which opens the DiscardedAll batch Toluz observes.
	resolveDiscardHand(t, e, 0)
	if got := zoneOfObj(e, fodder.ID); got != state.ZGraveyard {
		t.Fatalf("discarded card zone = %v, want graveyard (the discard must really run)", got)
	}
	drainMillTrigger(t, e, 40)

	if got := zoneOfObj(e, fodder.ID); got != state.ZExile {
		t.Fatalf("discarded card zone = %v, want exile (the captured card must be exiled)", got)
	}
	if got := e.G.Obj(tID).Zone; got != state.ZBattlefield {
		t.Fatalf("precondition broken by resolution: Toluz zone %v, want battlefield", got)
	}
}
