// Ticket cli-20260922T225140Z-68ca4d95, fix round 2: the CR 704.5j legend
// choice is a STATE-BASED-action decision, and the SBA pass can be reached
// from inside a combat-damage tail that then poses its own priority round.
// Before this fix that tail overwrote the legend ask and -- because the SBA
// pass halts while a batch is parked -- no state-based action ever applied
// again for the rest of the match. These tests pin the two halves: the
// combat-damage tails DEFER to the ask, and a parked batch whose ask is lost
// re-poses it rather than wedging.
package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// legendCombatTwin is a legendary body with a leaves-the-battlefield witness
// so a settled batch is observable in the log.
const legendCombatTwin = "Name:Combat Twin\nManaCost:2 G\nTypes:Legendary Creature Bear\nPT:5/5\nOracle:x\n"

// combatLegendBoard fields two identically-named legendary permanents under
// seat 0 and asserts the CR 704.5j precondition the rule reads: both on the
// battlefield, both legendary, same printed name, distinct ids. It returns the
// engine and the two ids in battlefield order.
func combatLegendBoard(t *testing.T) (*Engine, state.ObjID, state.ObjID) {
	t.Helper()
	e := layerEngine(t)
	id1 := onBoard(t, e, 0, legendCombatTwin)
	id2 := onBoard(t, e, 0, legendCombatTwin)
	for _, id := range []state.ObjID{id1, id2} {
		o := e.G.Obj(id)
		if o == nil || o.Zone != state.ZBattlefield {
			t.Fatalf("fixture: legendary permanent %d not on the battlefield (zone %v)", id, o)
		}
		if !o.Face().IsLegendary() || o.Face().Name != "Combat Twin" {
			t.Fatalf("fixture: %d is %q legendary=%v; the legend rule cannot fire",
				id, o.Face().Name, o.Face().IsLegendary())
		}
	}
	if id1 == id2 {
		t.Fatalf("fixture: both duplicates share id %d", id1)
	}
	return e, id1, id2
}

// TestLegendRuleSurvivesFirstStrikeCombatTail drives the first-strike combat
// damage tail (completeCombatPass(true)) on a board with a legend duplicate
// set. Before the fix that tail posed the between-passes priority round over
// the legend ask and wedged the SBA pass. The ask must be the surviving
// pending decision, and answering it must settle the batch.
func TestLegendRuleSurvivesFirstStrikeCombatTail(t *testing.T) {
	t.Parallel()
	e, id1, id2 := combatLegendBoard(t)
	e.completeCombatPass(true)

	d := e.Pending()
	if d == nil {
		t.Fatalf("no decision pending after the first-strike combat tail")
	}
	if d.Kind != decision.KChoose || len(d.Options) != 2 || d.Options[0].Kind != "keep" {
		t.Fatalf("the combat tail displaced the legend ask: pending %s (seq %d) with %d options",
			d.Kind, d.Seq, len(d.Options))
	}
	if e.legendBatch == nil {
		t.Fatalf("the legend batch is not parked under its ask")
	}
	submitKeep(t, e, d, 1)
	if o := e.G.Obj(id2); o.Zone != state.ZBattlefield {
		t.Fatalf("kept duplicate in %v, want battlefield", o.Zone)
	}
	if o := e.G.Obj(id1); o.Zone != state.ZGraveyard {
		t.Fatalf("unchosen duplicate in %v, want graveyard", o.Zone)
	}
	if e.legendBatch != nil {
		t.Fatalf("legend batch still parked after the answer")
	}
	// The deferred between-passes priority round (CR 510.4) must have run:
	// after the answer the engine owes a priority decision, not a stall.
	if p := e.Pending(); p == nil || p.Kind != decision.KPriority {
		t.Fatalf("the deferred between-passes priority round did not resume: pending %+v", p)
	}
}

// TestLegendRuleSurvivesRegularCombatTail is the regular-pass twin: the
// tail's end-of-combat transition must defer to the legend ask, and the
// transition must complete once the answer lands.
func TestLegendRuleSurvivesRegularCombatTail(t *testing.T) {
	t.Parallel()
	e, id1, id2 := combatLegendBoard(t)
	// The regular-pass tail's deferred transition lives in combatStep, which
	// runs only in the combat damage step.
	e.G.Step = state.StepCombatDamage
	e.completeCombatPass(false)

	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("the regular combat tail displaced the legend ask: pending %+v", d)
	}
	// The end-of-combat transition must NOT have run under the ask: setStep
	// skips its boundary cleanup while a decision is pending, so running it
	// early would strand the CR 511.3 end-combat reset. The guard defers the
	// whole transition to combatStep.
	if e.G.Step != state.StepCombatDamage {
		t.Fatalf("the end-of-combat transition ran under the outstanding ask: step %v", e.G.Step)
	}
	submitKeep(t, e, d, 1)
	if o := e.G.Obj(id1); o.Zone != state.ZGraveyard {
		t.Fatalf("unchosen duplicate in %v, want graveyard", o.Zone)
	}
	if o := e.G.Obj(id2); o.Zone != state.ZBattlefield {
		t.Fatalf("kept duplicate in %v, want battlefield", o.Zone)
	}
	// The deferred end-of-combat transition must have completed: the step
	// moved on and combatRound was cleared.
	if e.G.Step != state.StepEndCombat {
		t.Fatalf("deferred end-of-combat transition did not complete: step %v", e.G.Step)
	}
}

// TestLegendRuleReposesDisplacedAsk pins the safety net: a parked batch whose
// ask is gone (displaced by any future unguarded caller) is re-posed by the
// next SBA pass instead of wedging the whole SBA machinery. Simulating the
// loss by clearing the pending decision is deliberate -- the fix's guard makes
// the loss engine-unreachable in flow, so only a direct probe can exercise the
// recovery.
func TestLegendRuleReposesDisplacedAsk(t *testing.T) {
	t.Parallel()
	e, id1, id2 := combatLegendBoard(t)
	e.checkStateBased()
	first := e.Pending()
	if first == nil || first.Kind != decision.KChoose {
		t.Fatalf("fixture: the legend rule did not ask (pending %+v)", first)
	}
	// Simulate a displacement: the parked batch survives, the ask does not.
	e.pending = nil
	if e.legendBatch == nil {
		t.Fatalf("fixture: the batch is not parked; nothing to recover")
	}
	e.checkStateBased()
	again := e.Pending()
	if again == nil || again.Kind != decision.KChoose {
		t.Fatalf("the parked legend batch did not re-pose its ask: pending %+v", again)
	}
	if again.Seq == first.Seq {
		t.Fatalf("re-pose reused the displaced decision (seq %d); it must be a fresh ask", again.Seq)
	}
	// The recovered ask still resolves the batch.
	submitKeep(t, e, again, 1)
	if o := e.G.Obj(id1); o.Zone != state.ZGraveyard {
		t.Fatalf("recovered ask did not settle the batch: unchosen duplicate in %v", o.Zone)
	}
	if o := e.G.Obj(id2); o.Zone != state.ZBattlefield {
		t.Fatalf("recovered ask did not settle the batch: kept duplicate in %v", o.Zone)
	}
}

// TestLegendRuleDepartedControllerDeclines pins the CR 800.4a arm: a batch
// whose controller has left the game is settled deterministically (the
// battlefield-order first member kept, the rest binned) instead of posing a
// choice a departed seat can no longer make -- and instead of parking forever
// on the re-pose. A PlayerLost event is the real way to mark the seat; the
// direct askLegendChoice call keeps the scenario to the arm under test (the
// ordinary SBA sweep would already have removed a departed seat's board).
func TestLegendRuleDepartedControllerDeclines(t *testing.T) {
	t.Parallel()
	e, id1, id2 := combatLegendBoard(t)
	e.checkStateBased()
	if d := e.Pending(); d == nil || d.Kind != decision.KChoose {
		t.Fatalf("fixture: the legend rule did not ask (pending %+v)", d)
	}
	// The controller leaves the game with the ask outstanding.
	e.emit(events.Event{Kind: events.PlayerLost, Player: 0})
	if !e.G.Players[0].Lost {
		t.Fatalf("fixture: PlayerLost did not mark seat 0 lost")
	}
	e.pending = nil // what releasePendingDecisionOfDepartedPlayer would clear
	e.askLegendChoice()
	if e.Pending() != nil {
		t.Fatalf("a departed controller was still asked: pending %+v", e.Pending())
	}
	if e.legendBatch != nil {
		t.Fatalf("the batch was left parked for a departed controller")
	}
	if o := e.G.Obj(id1); o.Zone != state.ZBattlefield {
		t.Fatalf("decline kept the battlefield-order first member, got zone %v", o.Zone)
	}
	if o := e.G.Obj(id2); o.Zone != state.ZGraveyard {
		t.Fatalf("decline did not bin the unchosen duplicate, got zone %v", o.Zone)
	}
}
