package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// The FirstCombat$ trigger-gate leaf tests (Forge's Trigger.getFirstCombat /
// requirementsCheck "First Combat" gate: "if it's the first combat phase of
// the turn"). The gate lives in triggerMatches as a SHARED gate, so one real
// carrier pins every mode that rides the key; Finest Hour is the cleanest
// because its own trigger's Execute$ grants the extra combat the gate must
// then stay silent in -- the fire/silence pair the ticket asks for, end to
// end on the real corpus card.

// TestFinestHourFirstCombatTriggerFiresOncePerTurn (real corpus Finest
// Hour): the lone attack in the FIRST combat fires the trigger -- the
// attacker untaps and the trigger's own DB$ AddPhase grants an extra combat
// -- but the same trigger in that extra combat must not fire again (no
// untap, no second grant); it fires again on the NEXT turn's first combat.
// The whole game replays byte-identically.
func TestFinestHourFirstCombatTriggerFiresOncePerTurn(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := addPhaseEngine(t, reg,
		[]*cards.Card{lookup(t, reg, "Finest Hour"), card(t, bearSrc)}, []*cards.Card{})
	moveByName(t, e, 0, "Finest Hour", state.ZBattlefield)
	bear := moveByName(t, e, 0, "Bear", state.ZBattlefield)
	for driveToTurn(t, e, 3, 0) {
	}
	driveToStep(t, e, e.G.Turn, e.G.Active, state.StepDeclareAttackers)
	passToKind(t, e, decision.KAttackers)
	submitAttackersOnly(t, e, bear)
	answerTriggerOrders(t, e) // the attack queued Finest Hour's trigger + Exalted's
	passUntilStackEmpty(t, e, 40)
	// The FIRST combat: Finest Hour's trigger fired -- the attacking Bear was
	// tapped by the declaration and the trigger untapped it -- and its
	// chained AddPhase granted the extra combat. (Exalted's own keyword
	// trigger also fires here; it only pumps, it never untaps or grants.)
	if e.G.Obj(bear).Tapped {
		t.Fatal("Finest Hour's first-combat trigger did not untap the lone attacker")
	}
	if len(e.G.ExtraPhases) != 1 {
		t.Fatalf("ExtraPhases = %+v, want one grant from the first combat", e.G.ExtraPhases)
	}
	// Cross the first combat's end-of-combat step: the grant consumes and the
	// extra combat reaches its own attackers ask.
	passAll(t, e, 200)
	passToKind(t, e, decision.KAttackers)
	if e.G.CombatsThisTurn != 2 {
		t.Fatalf("CombatsThisTurn = %d, want 2 (first combat + the extra one)", e.G.CombatsThisTurn)
	}
	if !e.G.ExtraPhases[0].Consumed {
		t.Fatalf("grant not consumed at the extra combat: %+v", e.G.ExtraPhases[0])
	}
	// Attack again in the EXTRA combat: the FirstCombat$ gate must keep the
	// trigger silent -- no untap of the re-tapped attacker and no second
	// grant (before the gate this trigger re-fired in every combat, which is
	// the defect this leaf pins).
	submitAttackersOnly(t, e, bear)
	answerTriggerOrders(t, e)
	passUntilStackEmpty(t, e, 40)
	grants := countEvents(e, func(ev events.Event) bool {
		return ev.Kind == events.ExtraPhase && ev.Amount > 0
	})
	if grants != 1 {
		t.Fatalf("%d ExtraPhase grants after the extra combat's lone attack, want 1 (FirstCombat$ must silence the second)", grants)
	}
	if !e.G.Obj(bear).Tapped {
		t.Fatal("the extra combat fired an untap trigger; FirstCombat$ should have kept it silent")
	}
	// The extra combat ends and the turn resumes at Main2.
	driveToStep(t, e, 3, 0, state.StepMain2)
	if e.G.Step != state.StepMain2 || e.G.Turn != 3 {
		t.Fatalf("after the extra combat: step %s turn %d, want Main2 of turn 3", e.G.Step, e.G.Turn)
	}
	if len(e.G.ExtraPhases) != 0 {
		t.Fatalf("ExtraPhases not drained: %+v", e.G.ExtraPhases)
	}
	// The gate is per TURN, not per game: on the next turn of the same seat
	// the combat count has reset to 1, so the trigger fires again and grants
	// again. (Turn 4 is the opponent's; turn 5 is seat 0's next turn.)
	driveToStep(t, e, 5, 0, state.StepDeclareAttackers)
	passToKind(t, e, decision.KAttackers)
	submitAttackersOnly(t, e, bear)
	answerTriggerOrders(t, e)
	passUntilStackEmpty(t, e, 40)
	if e.G.Obj(bear).Tapped {
		t.Fatal("the next turn's first combat did not untap the attacker; the gate must be per turn, not per game")
	}
	if grants := countEvents(e, func(ev events.Event) bool {
		return ev.Kind == events.ExtraPhase && ev.Amount > 0
	}); grants != 2 {
		t.Fatalf("%d ExtraPhase grants after the next turn's lone attack, want 2", grants)
	}
	replayCheck(t, e, cfg)
}
