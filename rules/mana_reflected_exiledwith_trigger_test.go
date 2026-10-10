// Pit of Offerings' ManaReflected ability reads "Defined.ExiledWith": the
// cards exiled by the source. A REAL enters-the-battlefield trigger exiles
// cards through a ChangeZone body, which records them on the source's FORWARD
// ExiledCards list (Forge's hostCard.exiledCards) and deliberately does NOT
// stamp the reverse Object.ExiledWith scalar, because the script never names
// ExiledWithSource. The reflect scan must read both spellings (the shared
// exiledBySource association), or the ability is never offered even with a
// card exiled by it. Kept in its own file so the ticket cannot conflict on a
// shared test file.
package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// redRemnant is the graveyard card the Pit's ETB exiles; its colour is the
// one the reflect ability must then offer.
const redRemnant = "Name:Red Remnant\nManaCost:2 R\nTypes:Creature Goblin\nPT:2/2\nOracle:x\n"

// TestManaReflectedReadsRealETBExileForwardList drives the REAL trigger path
// (never a hand-emitted MoveZone IDs payload that stamps the reverse scalar):
// seat 0 has Pit of Offerings in hand and a red creature in the graveyard; the
// Pit is played for real, its ETB trigger exiles the creature, and only then
// is the reflect ability activated.
func TestManaReflectedReadsRealETBExileForwardList(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusAlternativeCard(t, "Pit of Offerings"))
	pit := e.G.Zone(state.ZHand, 0)[0]

	// Precondition: a red creature card sits in seat 0's graveyard, the zone
	// the ETB trigger exiles from.
	red := e.G.AddObject(card(t, redRemnant), 0)
	e.emit(events.Event{Kind: events.MoveZone, Obj: red.ID, From: state.ZLibrary, To: state.ZGraveyard})
	if o := e.G.Obj(red.ID); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("precondition: graveyard card zone = %+v, want Graveyard", o)
	}

	// Play the REAL Pit: a real hand->battlefield move runs its ETB trigger
	// (the ETBTapped replacement taps it as it enters).
	e.emit(events.Event{Kind: events.MoveZone, Obj: pit, From: state.ZHand, To: state.ZBattlefield})
	if o := e.G.Obj(pit); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Pit zone = %+v, want Battlefield", o)
	}
	e.putTriggersOnStack()
	if d := e.Pending(); d != nil && d.Kind == decision.KTarget {
		answerKTarget(t, e, red.ID)
	}
	e.resolveTop()

	// The ETB's real exile moved the card AND recorded it on the source's
	// forward ExiledCards list, not the reverse scalar -- the exact record
	// main's reflect scan could not read.
	if o := e.G.Obj(red.ID); o == nil || o.Zone != state.ZExile {
		t.Fatalf("precondition: ETB exile zone = %+v, want Exile", o)
	}
	forward := false
	for _, id := range e.G.Obj(pit).ExiledCards {
		if id == red.ID {
			forward = true
		}
	}
	if !forward {
		t.Fatalf("precondition: source forward ExiledCards = %v, want it to hold the exiled card", e.G.Obj(pit).ExiledCards)
	}
	if e.G.Obj(red.ID).ExiledWith != 0 {
		t.Fatalf("precondition: reverse ExiledWith = %d, want 0 (this script never names ExiledWithSource)", e.G.Obj(red.ID).ExiledWith)
	}

	// The Pit entered tapped; untap it so the {T} ability can be activated.
	e.emit(events.Event{Kind: events.Untap, Obj: pit})
	if e.G.Obj(pit).Tapped {
		t.Fatal("precondition: Pit still tapped after the untap event")
	}
	if got := e.G.Players[0].Pool.Total(); got != 0 {
		t.Fatalf("precondition: pool = %d, want 0 before the activation", got)
	}

	e.pending = nil
	e.priorityRound()
	activateManaReflected(t, e, pit)

	if got := e.G.Players[0].Pool[state.MR]; got != 1 {
		t.Fatalf("pool R = %d, want 1 (the exiled card's colour)", got)
	}
	if got := e.G.Players[0].TypedMana[state.TypedCave][state.MR]; got != 1 {
		t.Fatalf("Cave tally R = %d, want 1 (the Cave source's reflected unit is Cave mana)", got)
	}
}
