package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestDamageToAPlayerIsLifeLostThisTurn: damage dealt to a player causes
// that player to lose that much life (CR 120.3a, 119.3), but a player Damage
// event folds straight to the life total with no LifeChange. The
// life-lost-this-turn folds summed only LifeChange events, so every head
// over them (Count$LifeOppsLostThisTurn, LifeYouLostThisTurn, the
// PlayerCount*$LifeLostThisTurn / HasPropertyLostLifeThisTurn family and
// LifeLostLastTurn) read 0 after burn or combat damage. Infect damage to a
// player is poison, not life loss (CR 702.90b), and damage to an object is
// no player's life loss.
func TestDamageToAPlayerIsLifeLostThisTurn(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	bear := onBoardCard(t, e, 1, corpusCard(t, "Grizzly Bears"))
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: e.G.Turn + 1})
	e.emit(events.Event{Kind: events.Damage, Player: 1, Amount: 2})
	e.emit(events.Event{Kind: events.Damage, Player: 1, Amount: 3, Counter: "infect"})
	e.emit(events.Event{Kind: events.Damage, Obj: bear, Amount: 1, Counter: "creature"})
	e.emit(events.Event{Kind: events.LifeChange, Player: 1, Amount: -1})
	e.emit(events.Event{Kind: events.LifeChange, Player: 1, Amount: 7})
	if got := e.LifeLostThisTurn(1); got != 3 {
		t.Fatalf("LifeLostThisTurn(1) = %d, want 3 (2 damage + 1 lose-life; infect and the bear's damage are not life loss, the gain does not offset)", got)
	}
	if got := e.LifeLostThisTurn(0); got != 0 {
		t.Fatalf("LifeLostThisTurn(0) = %d, want 0", got)
	}
	if n := evalHead(t, e, 0, 0, "Count$LifeOppsLostThisTurn"); n != 3 {
		t.Fatalf("Count$LifeOppsLostThisTurn = %d, want 3", n)
	}
	if n := evalHead(t, e, 0, 0, "PlayerCountOpponents$HasPropertyLostLifeThisTurn"); n != 1 {
		t.Fatalf("PlayerCountOpponents$HasPropertyLostLifeThisTurn = %d, want 1", n)
	}
	e.emit(events.Event{Kind: events.TurnChange, Player: 1, Amount: e.G.Turn + 1})
	if n := evalHead(t, e, 1, 0, "PlayerCountPropertyYou$LifeLostLastTurn"); n != 3 {
		t.Fatalf("LifeLostLastTurn = %d, want 3 (damage counts last turn too)", n)
	}
	if got := e.LifeLostThisTurn(1); got != 0 {
		t.Fatalf("LifeLostThisTurn(1) after the turn ended = %d, want 0", got)
	}
}

// TestStromkirkBloodthiefCountsDamageAsLifeLoss: the end-step gate "if an
// opponent lost life this turn" (CheckSVar$ X, X = Count$LifeOppsLostThisTurn)
// is satisfied by damage alone -- the Oracle-audit scenarios where Shock or
// combat damage hit the opponent and no counter was placed.
func TestStromkirkBloodthiefCountsDamageAsLifeLoss(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	onBoardCard(t, e, 0, corpusCard(t, "Stromkirk Bloodthief"))
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: e.G.Turn + 1})
	if n := stepTriggers(e, state.StepEnd, 0); n != 0 {
		t.Fatalf("no life lost: end-step triggers = %d, want none", n)
	}
	e.pendingTriggers = nil
	e.emit(events.Event{Kind: events.Damage, Player: 1, Amount: 2})
	if n := stepTriggers(e, state.StepEnd, 0); n != 1 {
		t.Fatalf("opponent dealt 2 damage: end-step triggers = %d, want the Bloodthief's one", n)
	}
	e2 := layerEngine(t)
	onBoardCard(t, e2, 0, corpusCard(t, "Stromkirk Bloodthief"))
	e2.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: e2.G.Turn + 1})
	e2.emit(events.Event{Kind: events.Damage, Player: 1, Amount: 2, Counter: "infect"})
	if n := stepTriggers(e2, state.StepEnd, 0); n != 0 {
		t.Fatalf("infect damage only: end-step triggers = %d, want none (poison is not life loss)", n)
	}
}
