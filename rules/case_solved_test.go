package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// caseSolvedCase is a synthetic Case in the MKM cycle's exact Forge shape
// (Case of the Filched Falcon's "To solve -- You control three or more
// artifacts" narrowed to one artifact, Case of the Gateway Express's
// "Solved -- Creatures you control get +1/+0" static, and Case of the
// Stashed Skeleton's `Activation$ Solved` sacrifice ability).
const caseSolvedCase = `Name:Case of the Synthetic Probe
ManaCost:1 W
Types:Enchantment Case
T:Mode$ Phase | Phase$ End of Turn | ValidPlayer$ You | TriggerZones$ Battlefield | IsPresent$ Artifact.YouCtrl | PresentCompare$ GE1 | IsPresent2$ Card.Self+!IsSolved | Execute$ TrigSolve | TriggerDescription$ To solve — You control an artifact.
SVar:TrigSolve:DB$ AlterAttribute | Defined$ Self | Attributes$ Solved
S:Mode$ Continuous | Affected$ Creature.YouCtrl | AddPower$ 1 | IsPresent$ Card.Self+IsSolved | Description$ Solved — Creatures you control get +1/+0.
A:AB$ GainLife | Cost$ Sac<1/CARDNAME> | Activation$ Solved | LifeAmount$ 3 | PrecostDesc$ Solved — | SpellDescription$ You gain 3 life.
Oracle:To solve — You control an artifact.\nSolved — Creatures you control get +1/+0.\nSolved — Sacrifice this Case: You gain 3 life.
`

// caseFileAuditor is MKM Case File Auditor's CaseSolved trigger line
// verbatim; its Dig body is swapped for a GainLife so the test observes the
// trigger without driving the library-look decision.
const caseFileAuditor = `Name:Case File Auditor
ManaCost:2 W
Types:Creature Human Detective
PT:1/4
T:Mode$ CaseSolved | ValidPlayer$ You | ValidCard$ Case | Execute$ TrigGain | TriggerZones$ Battlefield | TriggerDescription$ Whenever you solve a Case, gain 1 life.
SVar:TrigGain:DB$ GainLife | Defined$ You | LifeAmount$ 1
Oracle:Whenever you solve a Case, gain 1 life.
`

// endStepTriggers fires the beginning-of-end-step event on seat 0's turn
// and returns how many triggers it queued.
func endStepTriggers(e *Engine) int {
	e.pending = nil
	e.pendingTriggers = nil
	e.G.Active = 0
	e.G.Step = state.StepEnd
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepEnd})
	return len(e.pendingTriggers)
}

// TestCaseBecomesSolvedAtEndStepAndAuditorTriggers pins CR 719.3a/b and
// CR 702.169: a Case whose solve condition fails at the end step stays
// unsolved; when it holds, the Case becomes solved through an
// events.AlterAttribute grant, its "Solved --" static and activated
// abilities turn on, Case File Auditor's "whenever you solve a Case" fires
// once, and an already-solved Case is never solved again.
func TestCaseBecomesSolvedAtEndStepAndAuditorTriggers(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	cs := onBoardCard(t, e, 0, card(t, caseSolvedCase))
	auditor := onBoardCard(t, e, 0, card(t, caseFileAuditor))
	ab := e.G.Obj(cs).Face().Abilities[0]
	if ab == nil || ab.ParamStr(cards.PKActivation) != "Solved" {
		t.Fatalf("fixture: the Case's activated ability is not the Activation$ Solved one: %+v", ab)
	}
	if got := e.Power(auditor); got != 1 {
		t.Fatalf("unsolved Case already pumps: Auditor power = %d, want 1", got)
	}
	if e.activationConditionOK(0, cs, ab) {
		t.Fatal("an unsolved Case offers its Solved ability")
	}

	// Condition fails: no artifact, so the intervening-if keeps the
	// solve trigger off the stack.
	if n := endStepTriggers(e); n != 0 {
		t.Fatalf("solve condition false but %d triggers queued", n)
	}

	onBoard(t, e, 0, "Name:Probe Bauble\nTypes:Artifact\nOracle:\n")
	if n := endStepTriggers(e); n != 1 {
		t.Fatalf("solve condition true: queued %d triggers, want the Case's solve trigger", n)
	}
	e.putTriggersOnStack()
	e.resolveTop()
	if !e.G.Obj(cs).Solved {
		t.Fatal("the Case is not solved after its solve trigger resolved")
	}
	grants := 0
	for _, ev := range e.L.Events {
		if events.IsAlterAttribute(ev, "Solved") {
			grants++
			if ev.Obj != cs || ev.Player != 0 || ev.Amount != 1 {
				t.Fatalf("Solved grant = %+v, want Obj %d Player 0 Amount 1", ev, cs)
			}
		}
		if ev.Kind == events.Note && ev.Obj == cs {
			t.Fatalf("solve body left a Note: %q", ev.Text)
		}
	}
	if grants != 1 {
		t.Fatalf("Solved grants = %d, want 1", grants)
	}

	// Case File Auditor's CaseSolved trigger is waiting.
	if len(e.pendingTriggers) != 1 {
		t.Fatalf("pending triggers after the solve = %d, want Case File Auditor's", len(e.pendingTriggers))
	}
	life := e.G.Players[0].Life
	e.putTriggersOnStack()
	e.resolveTop()
	if got := e.G.Players[0].Life; got != life+1 {
		t.Fatalf("Auditor's CaseSolved trigger did not resolve: life %d -> %d", life, got)
	}

	// The Solved abilities are on.
	if got := e.Power(auditor); got != 2 {
		t.Fatalf("solved Case's static: Auditor power = %d, want 2", got)
	}
	if !e.activationConditionOK(0, cs, ab) {
		t.Fatal("a solved Case withholds its Solved ability")
	}

	// A solved Case stays solved and is never solved again.
	if n := endStepTriggers(e); n != 0 {
		t.Fatalf("an already-solved Case queued %d triggers at the next end step", n)
	}
	if !e.G.Obj(cs).Solved {
		t.Fatal("the solved designation did not persist")
	}
}

// TestCaseSolvedClearsWhenTheCaseLeaves pins CR 719.3b's one end: a solved
// Case that leaves the battlefield loses the designation.
func TestCaseSolvedClearsWhenTheCaseLeaves(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	cs := onBoardCard(t, e, 0, card(t, caseSolvedCase))
	e.emit(events.Event{Kind: events.AlterAttribute, Obj: cs, Text: "Solved", Amount: 1})
	if !e.G.Obj(cs).Solved {
		t.Fatal("precondition: the Solved grant did not set the designation")
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: cs, From: state.ZBattlefield, To: state.ZGraveyard})
	if o := e.G.Obj(cs); o.Zone != state.ZGraveyard || o.Solved {
		t.Fatalf("after leaving: zone %v solved %v, want graveyard and unsolved", o.Zone, o.Solved)
	}
}
