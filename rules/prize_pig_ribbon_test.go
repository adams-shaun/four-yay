package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// Prize Pig's ribbon payoff (the api:RemoveCounter pin): "Whenever you gain
// life, put that many ribbon counters on Prize Pig. Then if there are three
// or more ribbon counters on Prize Pig, remove those counters and untap it."
//
// Before the RemoveCounter primitive was registered (effects/counters.go)
// the removal sub-ability hit the generic "unimplemented API" Note, and
// before the LifeGained trigger mode existed (rules/trigger_match.go) the
// card's own trigger never fired at all. Both shapes are pinned here end to
// end on the real corpus card, replay-verified like every engine test.

// prizePigTable seats the prepared decks with Prize Pig on seat 0's
// battlefield and drives to seat 0's first priority ask.
func prizePigTable(t *testing.T, seed uint64) (*Engine, Config, state.ObjID) {
	t.Helper()
	pig := corpusCard(t, "Prize Pig")
	e, cfg := putCounterTable(t, seed,
		[]*cards.Card{pig},
		[]*cards.Card{})
	pigID := findAndMoveToBattlefield(t, e, 0, "Prize Pig")
	e.priorityRound()
	return e, cfg, pigID
}

// gainLife emits the positive LifeChange the LifeGained trigger reads,
// refreshes priority (the point the queued trigger pushes onto the stack)
// and drains the stack.
func gainLife(t *testing.T, e *Engine, p state.PlayerID, amount int32) {
	t.Helper()
	e.emit(events.Event{Kind: events.LifeChange, Player: p, Amount: amount})
	e.priorityRound()
	passUntilStackEmpty(t, e, 30)
}

func TestPrizePigRemovesThreeRibbonsAndUntaps(t *testing.T) {
	t.Parallel()
	e, cfg, pig := prizePigTable(t, 311)
	// Tap the Pig first so the untap payoff is observable.
	e.emit(events.Event{Kind: events.Tap, Obj: pig})
	if !e.G.Obj(pig).Tapped {
		t.Fatal("Prize Pig did not tap")
	}
	gainLife(t, e, 0, 3)
	if got := e.G.Obj(pig).Counter("RIBBON"); got != 0 {
		t.Fatalf("RIBBON counters = %d, want 0 (three or more removes them)", got)
	}
	if e.G.Obj(pig).Tapped {
		t.Fatal("Prize Pig still tapped after the payoff untapped it")
	}
	if got := len(e.G.Obj(pig).Remembered); got != 0 {
		t.Fatalf("remembered size = %d, want 0 (Cleanup's ClearRemembered$)", got)
	}
	replayCheck(t, e, cfg)
}

// Below the threshold: 2 life puts 2 ribbon counters, the removal gate fails,
// nothing is removed and the Pig stays tapped.
func TestPrizePigBelowThresholdKeepsCountersAndTap(t *testing.T) {
	t.Parallel()
	e, _, pig := prizePigTable(t, 311)
	e.emit(events.Event{Kind: events.Tap, Obj: pig})
	gainLife(t, e, 0, 2)
	if got := e.G.Obj(pig).Counter("RIBBON"); got != 2 {
		t.Fatalf("RIBBON counters = %d, want 2 (below three keeps them)", got)
	}
	if !e.G.Obj(pig).Tapped {
		t.Fatal("Prize Pig untapped below the threshold")
	}
}

// The counters accumulate across triggers: 1 life then 2 life leaves three on
// the second trigger, which removes them and untaps.
func TestPrizePigAccumulatesAcrossTriggers(t *testing.T) {
	t.Parallel()
	e, _, pig := prizePigTable(t, 311)
	e.emit(events.Event{Kind: events.Tap, Obj: pig})
	gainLife(t, e, 0, 1)
	if got := e.G.Obj(pig).Counter("RIBBON"); got != 1 {
		t.Fatalf("RIBBON counters = %d, want 1 after the first gain", got)
	}
	gainLife(t, e, 0, 2)
	if got := e.G.Obj(pig).Counter("RIBBON"); got != 0 {
		t.Fatalf("RIBBON counters = %d, want 0 (1+2 = 3 crosses the threshold)", got)
	}
	if e.G.Obj(pig).Tapped {
		t.Fatal("Prize Pig still tapped after the accumulated payoff")
	}
}
