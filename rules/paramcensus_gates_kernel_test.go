package rules

// Kernel-era restorations of the tests W3 removed from paramcensus_gates_test.go: the
// same behaviour driven through the resolution kernel (the only ask path).

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestDesecrationDemonMultiTargetOptionalDoesNotWedge pins the Optional$
// ask's per-target binding on the real corpus card the r2 review wedged with:
// with TWO opponents, Defined$ Opponent + Optional$ True + a real host made
// the shared "sacrifice" resume arm re-run effSacrifice's target walk, target
// 1 consumed target 2's answer, and seat 2 was re-asked forever (11
// consecutive asks before the probe capped). The merged engine binds each
// ask to its exact target (ResumeTarget), so the ask RUNS -- each targeted
// opponent is asked in turn -- and the table does not wedge: a decline moves
// on to the next target's own ask, an accepted sacrifice is remembered only
// for the target that took it, and the RememberSacrificed$ chain (tap +
// P1P1 counter, gated on Remembered$Amount) fires for the sacrifice that
// happened.
func TestDesecrationDemonMultiTargetOptionalDoesNotWedgeKernel(t *testing.T) {
	t.Parallel()
	victim := "Name:Victim\nTypes:Creature\nPT:1/1\nOracle:x\n"
	fixture := choiceCorpusCard(t, "Desecration Demon")
	cfg := seatZeroStart(Config{Seed: 908, Names: []string{"a", "b", "c"},
		Decks: [][]*cards.Card{
			append([]*cards.Card{fixture}, mountainDeck(t, 39)...),
			append([]*cards.Card{card(t, victim)}, mountainDeck(t, 39)...),
			append([]*cards.Card{card(t, victim)}, mountainDeck(t, 39)...),
		},
		Tokens: map[string]*cards.Card{},
	})
	e := New(cfg)
	e.Advance()
	demon := findInZones(t, e, 0, "Desecration Demon")
	if demon == 0 {
		t.Fatal("Desecration Demon not in seat 0's hand or library")
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: demon, From: state.ZHand, To: state.ZBattlefield})
	var victims []state.ObjID
	for _, p := range []state.PlayerID{1, 2} {
		id := findInZones(t, e, p, "Victim")
		if id == 0 {
			t.Fatalf("Victim not in seat %d's hand or library", p)
		}
		e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZBattlefield})
		victims = append(victims, id)
	}
	e.emit(events.Event{Kind: events.TriggerPush, Obj: demon, Player: 0, Amount: 0})
	e.pending = nil // resolve directly, outside the pending priority window
	e.resolveTop()
	// Seat 1 is asked first: a 0..1 may-ask over its own Victim.
	d1 := e.Pending()
	if d1 == nil || d1.Kind != decision.KChoose || d1.Player != 1 || d1.Min != 0 || d1.Max != 1 || len(d1.Options) != 1 || d1.Options[0].Obj != victims[0] {
		t.Fatalf("seat 1's optional sacrifice ask missing or wrong: %+v", d1)
	}
	submitChoices(t, e) // the empty answer declines
	// Seat 2 then gets its OWN ask (the per-target binding): the decline did
	// not consume it and it is not a re-ask of seat 1.
	d2 := e.Pending()
	if d2 == nil || d2.Kind != decision.KChoose || d2.Player != 2 || d2.Min != 0 || d2.Max != 1 || len(d2.Options) != 1 || d2.Options[0].Obj != victims[1] {
		t.Fatalf("seat 2's optional sacrifice ask missing or wrong: %+v", d2)
	}
	submitChoices(t, e, 0) // seat 2 accepts: sacrifice its Victim
	// Resolution completed -- no third ask, no wedge. A priority decision is
	// the ordinary post-resolution game flow, not an ask.
	if d := e.Pending(); d != nil && d.Kind != decision.KPriority {
		t.Fatalf("an ask decision is still pending after both targets answered: %+v", d)
	}
	if z := e.G.Obj(victims[0]).Zone; z != state.ZBattlefield {
		t.Fatalf("victim 1 zone %v, want kept (seat 1 declined)", z)
	}
	if z := e.G.Obj(victims[1]).Zone; z != state.ZGraveyard {
		t.Fatalf("victim 2 zone %v, want sacrificed (seat 2 accepted)", z)
	}
	dm := e.G.Obj(demon)
	if !dm.Tapped || dm.Counter("P1P1") != 1 {
		t.Fatalf("demon tapped=%v P1P1=%d, want tapped +1 (a sacrifice happened)", dm.Tapped, dm.Counter("P1P1"))
	}
	replayCheck(t, e, cfg)
}
