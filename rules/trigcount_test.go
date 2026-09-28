package rules

// Task trigcount1: the TriggerCount$ heads (SVar bodies like
// TriggerCount$DamageAmount) were unimplemented, so "whenever CARDNAME deals
// damage, you gain that much life" resolved as zero. These tests pin the
// end-to-end fix through the real trigger machinery: a Damage event fires a
// DamageDone trigger, whose ability resolves with Ctx.TriggerAmount carrying
// the amount the triggering event dealt, so the gain (or any other "that
// much" sized effect) is N rather than 0.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestTriggerCountDamageAmountGainsThatMuchLife is the brief's primary leaf.
// The card is the exact shape the brief names -- a creature with "whenever
// this deals damage to a player, you gain that much life". It is synthetic
// because every real corpus card with this wording is not currently able to
// fire through the engine: it either uses the unregistered DamageDoneOnce
// mode (Metropolis Reformer, Kami of the Honored Dead) or a ValidSource$
// the engine's damageSource() cannot satisfy for non-ability damage
// (Kjeldoran Gargoyle) -- see the report's Issues section. The SVar body
// (TriggerCount$DamageAmount) and the GainLife effect are the real ones; the
// fix under test is that the head resolves to the event's amount instead of
// 0.
func TestTriggerCountDamageAmountGainsThatMuchLife(t *testing.T) {
	t.Parallel()
	probe := card(t, `Name:Trigcount lifelink probe
Types:Creature Vampire
PT:3/3
T:Mode$ DamageDone | ValidTarget$ Player | Execute$ TrigGain | TriggerDescription$ Whenever CARDNAME deals damage to a player, you gain that much life.
SVar:TrigGain:DB$ GainLife | Defined$ You | LifeAmount$ X
SVar:X:TriggerCount$DamageAmount
Oracle:synthetic
`)
	// Two different N values in separate cases: the bug is a silent zero, and
	// a single-value test could pass by coincidence.
	for _, n := range []int32{2, 4} {
		t.Run("", func(t *testing.T) {
			deck := append(mountainDeck(t, 40), probe)
			e := New(Config{Seed: 42, Names: []string{"source", "target"},
				Decks: [][]*cards.Card{deck, mountainDeck(t, 40)}})
			e.Advance()
			id := crAbortMove(t, e, 0, "Trigcount lifelink probe", state.ZBattlefield)
			e.pending = nil
			before := e.G.Players[0].Life
			e.damaging = id
			e.emit(events.Event{Kind: events.Damage, Player: 1, Amount: n})
			e.damaging = 0
			if len(e.pendingTriggers) != 1 {
				t.Fatalf("pendingTriggers = %d, want 1 (deal-damage trigger)", len(e.pendingTriggers))
			}
			e.putTriggersOnStack()
			if len(e.G.Stack) != 1 {
				t.Fatalf("stack = %v, want the triggered ability", e.G.Stack)
			}
			e.resolveTop()
			if got := e.G.Players[0].Life - before; got != n {
				t.Fatalf("seat 0 gained %d life after dealing %d damage, want %d (the trigger must gain that much)", got, n, n)
			}
		})
	}
}
