package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// A real Cultivate search by seat 0 is observed by River Song controlled by
// seat 1. The search marker is emitted once for the completed search, not once
// per card moved or shuffled, and queues exactly one SearchedLibrary trigger.
func TestRiverSongOpponentSearchFiresOnce(t *testing.T) {
	t.Parallel()
	wilds := tokenReplCorpusCard(t, "Evolving Wilds")
	river := tokenReplCorpusCard(t, "River Song")
	e, cfg := tokenReplGameSeats(t, 109, []*cards.Card{wilds}, []*cards.Card{river})
	riverID := moveSeededCard(t, e, 1, river, state.ZBattlefield)
	if o := e.G.Obj(riverID); o == nil || o.Zone != state.ZBattlefield || o.Controller != 1 {
		t.Fatalf("precondition: River Song must be on seat 1's battlefield: %+v", o)
	}
	wildsID := moveSeededCard(t, e, 0, wilds, state.ZBattlefield)
	addMana(t, e, 0, "") // establish the active player's priority decision
	d := activateSearch(t, e, wildsID)
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "search" || d.Max < 1 {
		t.Fatalf("Evolving Wilds did not reach its library-search choice: %+v", d)
	}
	if len(d.Options) < 1 {
		t.Fatal("precondition: Evolving Wilds search must offer at least one library card")
	}
	// Pick one basic land, then let the search finish.
	submitChoices(t, e, d.Options[0].Index)
	searched, pushed := 0, 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.SearchedLibrary && ev.Player == 0 {
			searched++
		}
		if ev.Kind == events.TriggerPush && ev.Obj == riverID {
			pushed++
		}
	}
	if searched != 1 {
		t.Fatalf("completed Evolving Wilds search emitted %d SearchedLibrary markers, want 1", searched)
	}
	if pushed != 1 {
		t.Fatalf("River Song received %d trigger pushes for one opponent search, want 1", pushed)
	}
	replayCheck(t, e, cfg)
}
