package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestCombatDamageReplacementAsksQueueInEventOrder pins the cardfuzz panic
// "ask overwrote a suspended resolution's pending decision": combat damage is
// emitted one Damage event per assignment OUTSIDE any resolution pass, and a
// damage replacement whose ReplaceWith$ body asks (Nefarious Lich's hidden
// "exile that many cards from your graveyard" pick) suspended on the first
// attacker's damage and then asked AGAIN on the second's while the first was
// unanswered. The second ask must be deferred behind the first, and both
// bodies -- including the rest of each body's SubAbility$ chain -- must run.
func TestCombatDamageReplacementAsksQueueInEventOrder(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := layerEngine(t)
	onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Nefarious Lich"))
	a := onBoardCard(t, e, 1, mustCorpusCard(t, reg, "Grizzly Bears"))
	b := onBoardCard(t, e, 1, mustCorpusCard(t, reg, "Grizzly Bears"))
	for i := 0; i < 5; i++ {
		id := e.G.Zone(state.ZLibrary, 0)[0]
		e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZLibrary, To: state.ZGraveyard})
	}
	life := e.G.Players[0].Life
	e.combatRound.assignments = []assignment{
		{from: a, toPlayer: 0, amount: 2},
		{from: b, toPlayer: 0, amount: 2},
	}
	e.runCombatAssignments()

	for round := 0; round < 2; round++ {
		d := e.Pending()
		if d == nil || d.Kind != decision.KChoose || d.Player != 0 {
			t.Fatalf("round %d: want seat 0's graveyard pick, got %+v", round, d)
		}
		if d.Min != 2 || len(d.Options) < 2 {
			t.Fatalf("round %d: want a pick of exactly 2, got min=%d max=%d options=%d", round, d.Min, d.Max, len(d.Options))
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: 0, Choices: []int{0, 1}}); err != nil {
			t.Fatalf("round %d: submit: %v", round, err)
		}
	}
	if d := e.Pending(); d != nil && d.Kind == decision.KChoose {
		t.Fatalf("a third graveyard pick was posed: %+v", d)
	}
	if got := len(e.G.Zone(state.ZExile, 0)); got != 4 {
		t.Fatalf("exiled %d cards from seat 0's graveyard, want 4 (2 per damage event)", got)
	}
	if got := len(e.G.Zone(state.ZGraveyard, 0)); got != 1 {
		t.Fatalf("graveyard holds %d cards, want 1", got)
	}
	if e.G.Players[0].Life != life {
		t.Fatalf("life changed %d -> %d; the damage should have been replaced", life, e.G.Players[0].Life)
	}
	if e.G.Players[0].Lost {
		t.Fatal("seat 0 lost although it could exile enough cards")
	}
}
