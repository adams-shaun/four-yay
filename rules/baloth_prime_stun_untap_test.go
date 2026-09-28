package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestBalothPrimeSacrificeTriggerRemovesAStunCounterInsteadOfUntapping pins
// the reported behaviour of feedback 20260921T204701Z on the card's real
// corpus script. Baloth Prime enters tapped with six stun counters, and its
// "Whenever you sacrifice a land ... untap this creature" trigger runs a
// DB$ Untap. CR 122.1d replaces that untap with removing one stun counter, so
// the sacrifice trigger decrements STUN by one and the creature STAYS tapped
// (it untaps only once its sixth sacrifice clears the last counter). The
// report read the missing untap as an engine defect; it is the printed rule.
func TestBalothPrimeSacrificeTriggerRemovesAStunCounterInsteadOfUntapping(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := layerEngine(t)

	baloth := enterFromHand(t, e, reg, 0, "Baloth Prime")
	if o := e.G.Obj(baloth); !o.Tapped || o.Counter("STUN") != 6 {
		t.Fatalf("fixture: Baloth Prime tapped/stun = %v/%d, want tapped with six stun counters", o.Tapped, o.Counter("STUN"))
	}

	// Sacrifice a land: the real T:Mode$ Sacrificed | ValidCard$ Land trigger
	// observes the MoveZone label, exactly as TestSacrificedTriggerMayhemDevil
	// drives it. The land itself need not be a corpus card -- only the trigger
	// source is what this test pins.
	land := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Forest"))
	e.emit(events.Event{Kind: events.MoveZone, Obj: land, From: state.ZBattlefield,
		To: state.ZGraveyard, Text: "sacrificed"})
	requireOneEventTrigger(t, e, "Baloth Prime")

	e.putTriggersOnStack()
	e.resolveTop()

	o := e.G.Obj(baloth)
	if got := o.Counter("STUN"); got != 5 {
		t.Fatalf("stun after one sacrifice = %d, want 5 (CR 122.1d removes one instead of untapping)", got)
	}
	if !o.Tapped {
		t.Fatal("Baloth Prime untapped; a stun counter should have replaced the untap (CR 122.1d)")
	}
	found := false
	for _, ev := range e.L.Events {
		if ev.Kind == events.CounterChange && ev.Obj == baloth && ev.Counter == "STUN" && ev.Amount == -1 {
			found = ev.Text == events.UntapReplacedByStunNotice
		}
	}
	if !found {
		t.Fatal("Baloth Prime substitution event lacks CR 122.1d provenance marker")
	}
}

// TestBalothPrimeUntapsOnceTheLastStunCounterIsGone is the other half of
// CR 122.1d: the seventh sacrifice finds no stun counter left and the DB$
// Untap finally untaps the creature. Driven from the real script, six stun
// counters placed the way the card's own ETB does.
func TestBalothPrimeUntapsOnceTheLastStunCounterIsGone(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := layerEngine(t)

	baloth := enterFromHand(t, e, reg, 0, "Baloth Prime")
	// Consume all six counters through the trigger itself, then one more.
	for i := 0; i < 6; i++ {
		sacrificeALand(t, e, reg)
	}
	if o := e.G.Obj(baloth); o.Counter("STUN") != 0 || !o.Tapped {
		t.Fatalf("after six sacrifices stun/tapped = %d/%v, want 0/still tapped", o.Counter("STUN"), o.Tapped)
	}
	sacrificeALand(t, e, reg)
	if o := e.G.Obj(baloth); o.Tapped {
		t.Fatal("the seventh sacrifice should finally untap Baloth Prime")
	}
}

// sacrificeALand queues and resolves one "you sacrifice a land" trigger on the
// board's current Baloth Prime.
func sacrificeALand(t *testing.T, e *Engine, reg *cards.Registry) {
	t.Helper()
	land := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Forest"))
	e.emit(events.Event{Kind: events.MoveZone, Obj: land, From: state.ZBattlefield,
		To: state.ZGraveyard, Text: "sacrificed"})
	e.putTriggersOnStack()
	e.resolveTop()
}
