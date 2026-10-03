package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/rules/trigmatch"
	"github.com/adams-shaun/gorge/state"
)

// Scourge of the Throne's attack trigger (the corpus's one Condition$
// AttackedPlayerWithMostLife carrier):
//
//	T:Mode$ Attacks | ValidCard$ Creature.Self | Execute$ TrigUntap |
//	    FirstAttack$ True | Condition$ AttackedPlayerWithMostLife
//
// "Whenever CARDNAME attacks for the first time each turn, if it's attacking
// the player with the most life or tied for most life, untap all attacking
// creatures. After this phase, there is an additional combat phase."
//
// FirstAttack$ was already gated (rules/trigmatch_combat.go firstAttackOK,
// commit 8fb7d1d2); the intervening-if on the ATTACKED player's life was the
// unread half -- before the read, the trigger fired on EVERY attack of the
// first declaration regardless of the defender's standing.

// scourgeSeat builds a three-seat table with the real corpus Scourge of the
// Throne on seat 1, ready to attack, active on seat 1 in the
// declare-attackers step, with the seat life totals the caller names (the
// same direct-setup convention combat_test.go's fixtures use).
func scourgeSeat(t *testing.T, lives [3]int32) (*Engine, state.ObjID) {
	t.Helper()
	e := threeSeatEngine(t)
	for i, l := range lives {
		e.G.Players[i].Life = l
	}
	scourge := onBoardCard(t, e, 1, mshCorpusCard(t, "Scourge of the Throne"))
	e.G.Obj(scourge).SummonSick = false
	e.G.Active = 1
	e.G.Step = state.StepDeclareAttackers
	return e, scourge
}

// drainScourgeCombat answers every decision between the declaration and the
// trigger's resolution: priority passes (combat_test.go's drainCombatPriority
// shape) plus the trigger_order ask the card's OWN Dethrone trigger and the
// Attacks trigger raise together when both fire (two options, choose the
// first). Plain drainCombatPriority stalls on that ask.
func drainScourgeCombat(t *testing.T, e *Engine) {
	t.Helper()
	for i := 0; i < 128; i++ {
		d := e.Pending()
		if d == nil || e.G.Step != state.StepDeclareAttackers {
			// The step guard stops the drain at the declare-blockers
			// boundary -- past it the resolved trigger's own AddPhase grant
			// opens the NEXT combat's attackers decision, which is not
			// this fixture's business.
			return
		}
		switch d.Kind {
		case decision.KPriority:
			idx := -1
			for _, o := range d.Options {
				if o.Kind == "pass" {
					idx = o.Index
				}
			}
			if idx < 0 {
				return
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
				t.Fatalf("pass: %v", err)
			}
		case decision.KTriggerOrder:
			// Min == Max == n: the decision wants a FULL ordering of the
			// queued triggers (decision.go's distinct-index shape).
			choices := make([]int, 0, len(d.Options))
			for _, o := range d.Options {
				choices = append(choices, o.Index)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: choices}); err != nil {
				t.Fatalf("trigger order: %v", err)
			}
		default:
			t.Fatalf("unexpected decision kind %v (resume %q) while resolving the attack triggers", d.Kind, d.ResumeKind)
		}
	}
}

// TestScourgeOfTheThroneUntapsAttackingCreaturesAtMostLifeDefender is the
// positive half: seat 0 holds strictly the most life, Scourge attacks it, and
// the trigger untaps every attacking creature (Scourge itself) and grants the
// additional combat phase (the AddPhase SubAbility).
func TestScourgeOfTheThroneUntapsAttackingCreaturesAtMostLifeDefender(t *testing.T) {
	t.Parallel()
	e, scourge := scourgeSeat(t, [3]int32{30, 20, 20})

	e.askAttackers()
	submitAttackerAt(t, e, scourge, 0)
	if o := e.G.Obj(scourge); !o.Tapped {
		t.Fatal("precondition: the attacker must be tapped by the declaration before the trigger can untap it")
	}
	drainScourgeCombat(t, e)

	if o := e.G.Obj(scourge); o.Tapped {
		t.Fatal("Scourge of the Throne stayed tapped: the Condition$ AttackedPlayerWithMostLife trigger did not untap it")
	}
	if len(e.G.ExtraPhases) != 1 {
		t.Fatalf("len(ExtraPhases) = %d, want 1 (the trigger's AddPhase SubAbility)", len(e.G.ExtraPhases))
	}
}

// TestScourgeOfTheThroneFiresOnATiedMostLifeDefender pins "or tied for most
// life": seat 0 ties seat 2 at 20 and the trigger still fires.
func TestScourgeOfTheThroneFiresOnATiedMostLifeDefender(t *testing.T) {
	t.Parallel()
	e, scourge := scourgeSeat(t, [3]int32{20, 20, 20})

	e.askAttackers()
	submitAttackerAt(t, e, scourge, 0)
	drainScourgeCombat(t, e)

	if o := e.G.Obj(scourge); o.Tapped {
		t.Fatal("a tied-for-most-life defender must not veto the trigger")
	}
}

// TestScourgeOfTheThroneDoesNotFireOnALessLifedDefender is the intervening-if
// negative: seat 0 has the LEAST life, so "attacking the player with the most
// life" is false and the trigger never fires.
func TestScourgeOfTheThroneDoesNotFireOnALessLifedDefender(t *testing.T) {
	t.Parallel()
	e, scourge := scourgeSeat(t, [3]int32{10, 20, 20})

	e.askAttackers()
	submitAttackerAt(t, e, scourge, 0)
	drainScourgeCombat(t, e)

	if o := e.G.Obj(scourge); !o.Tapped {
		t.Fatal("Scourge untapped on a defender WITHOUT the most life -- the Condition$ gate did not hold")
	}
	if len(e.G.ExtraPhases) != 0 {
		t.Fatalf("len(ExtraPhases) = %d, want 0 (no trigger, no extra combat)", len(e.G.ExtraPhases))
	}
}

// TestScourgeOfTheThroneFirstAttackOnly pins the FirstAttack$ half on the
// same real card: a SECOND attack in the same turn (an extra combat re-enters
// the declare-attackers step without a TurnChange, so AttacksThisTurn is not
// reset) must not fire even with the condition satisfied.
func TestScourgeOfTheThroneFirstAttackOnly(t *testing.T) {
	t.Parallel()
	e, scourge := scourgeSeat(t, [3]int32{30, 20, 20})

	// First attack: fires (the positive half, re-asserted here so the second
	// attack below has something to differ from).
	e.askAttackers()
	submitAttackerAt(t, e, scourge, 0)
	drainScourgeCombat(t, e)
	if o := e.G.Obj(scourge); o.Tapped {
		t.Fatal("first attack must fire (untap)")
	}
	if got := e.G.Obj(scourge).AttacksThisTurn; got != 1 {
		t.Fatalf("AttacksThisTurn = %d, want 1 after the first declaration", got)
	}

	// Second attack in the same turn: re-enter the declare-attackers step
	// exactly as an extra combat does (no TurnChange between).
	e.G.Step = state.StepDeclareAttackers
	e.askAttackers()
	submitAttackerAt(t, e, scourge, 0)
	drainScourgeCombat(t, e)
	if got := e.G.Obj(scourge).AttacksThisTurn; got != 2 {
		t.Fatalf("AttacksThisTurn = %d, want 2 after the second declaration", got)
	}
	if o := e.G.Obj(scourge); !o.Tapped {
		t.Fatal("Scourge untapped on its SECOND attack -- the FirstAttack$ gate did not hold")
	}
}

// reachScourgeStack answers any trigger_order ask raised by the declaration
// (Dethrone plus the Attacks trigger queue together) and returns once the
// trigger is ON THE STACK with a priority decision pending -- the CR 603.4
// response window the resolution-time recheck exists for. A precondition
// failure is loud: without a non-empty stack the regression below would
// assert nothing about resolution.
func reachScourgeStack(t *testing.T, e *Engine) *decision.Decision {
	t.Helper()
	for i := 0; i < 64; i++ {
		d := e.Pending()
		if d == nil {
			t.Fatal("no pending decision while reaching the trigger's stack window")
		}
		switch d.Kind {
		case decision.KTriggerOrder:
			choices := make([]int, 0, len(d.Options))
			for _, o := range d.Options {
				choices = append(choices, o.Index)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: choices}); err != nil {
				t.Fatalf("trigger order: %v", err)
			}
		case decision.KPriority:
			if len(e.G.Stack) == 0 {
				t.Fatal("precondition: a priority window was offered with an empty stack; the trigger never queued")
			}
			return d
		default:
			t.Fatalf("unexpected decision kind %v while reaching the trigger's stack window", d.Kind)
		}
	}
	t.Fatal("did not reach the trigger's stack window within the decision budget")
	return nil
}

// TestScourgeOfTheThroneInterveningIfRecheckedAtResolution is the regression
// for the CR 603.4 resolution-time half: the defender holds the most life when
// Scourge attacks (so the trigger QUEUES), then loses the lead during the
// response window -- the intervening-if is false as the ability resolves, so
// it must be removed from the stack and do nothing. Before the fix the
// resolution check read no AttackedPlayerWithMostLife clause at all, so the
// queued trigger still untapped the attackers and granted the extra combat.
func TestScourgeOfTheThroneInterveningIfRecheckedAtResolution(t *testing.T) {
	t.Parallel()
	e, scourge := scourgeSeat(t, [3]int32{30, 20, 20})

	e.askAttackers()
	submitAttackerAt(t, e, scourge, 0)
	if o := e.G.Obj(scourge); !o.Tapped {
		t.Fatal("precondition: the attacker must be tapped by the declaration")
	}
	// The trigger queued because seat 0 held the most life (30 >= 20).
	reachScourgeStack(t, e)
	stacked := len(e.G.Stack)
	if stacked == 0 {
		t.Fatal("precondition: the attack trigger must be on the stack before the window")
	}
	// The defender loses the lead while the trigger waits to resolve; seat 2
	// is now the strict leader, so "attacking the player with the most life"
	// is false at resolution.
	e.G.Players[0].Life = 10
	e.G.Players[2].Life = 25

	drainScourgeCombat(t, e)

	if o := e.G.Obj(scourge); !o.Tapped {
		t.Fatal("the queued trigger untapped Scourge after its intervening-if became false at resolution")
	}
	if len(e.G.ExtraPhases) != 0 {
		t.Fatalf("len(ExtraPhases) = %d, want 0 (the fizzled trigger must grant no extra combat)", len(e.G.ExtraPhases))
	}
	if len(e.G.Stack) != 0 {
		t.Fatalf("len(Stack) = %d, want 0 after the fizzled ability left the stack", len(e.G.Stack))
	}
}

// TestAttacksConditionMostLifeMatcher drives attacksMatches directly on the
// real card's trigger line, splitting the exact multiplayer edges of the
// gate: strictly-most, tied, vetoed by a third seat, vetoed by death, and a
// dead larger total that must NOT veto.
func TestAttacksConditionMostLifeMatcher(t *testing.T) {
	t.Parallel()
	e := threeSeatEngine(t)
	scourge := onBoardCard(t, e, 1, mshCorpusCard(t, "Scourge of the Throne"))
	e.G.Obj(scourge).SummonSick = false

	trig := cards.Trigger{Mode: "Attacks", Params: map[string]string{
		"ValidCard": "Creature.Self", "Condition": "AttackedPlayerWithMostLife",
	}}
	evt := func(def state.PlayerID) events.Event {
		return events.Event{Kind: events.DeclareAttackers, Player: def, IDs: []state.ObjID{scourge}}
	}
	setLives := func(a, b, c int32) { e.G.Players[0].Life, e.G.Players[1].Life, e.G.Players[2].Life = a, b, c }

	// Strictly most: fires.
	setLives(30, 20, 20)
	if !trigmatch.AttacksMatches(boardOf(e), trig, scourge, evt(0)) {
		t.Fatal("defender with strictly the most life must match")
	}
	// Tied for most: fires.
	setLives(20, 20, 20)
	if !trigmatch.AttacksMatches(boardOf(e), trig, scourge, evt(0)) {
		t.Fatal("defender tied for the most life must match")
	}
	// A third seat strictly above the defender: must NOT match.
	setLives(10, 20, 25)
	if trigmatch.AttacksMatches(boardOf(e), trig, scourge, evt(0)) {
		t.Fatal("defender below another seat's life must not match")
	}
	// A dead seat holding the larger total must NOT veto: only LIVING seats
	// count against the comparison. The defender (seat 0) is the highest
	// LIVING total; the dead seat 2 holds 35 above it.
	setLives(30, 20, 35)
	e.G.Players[2].Lost = true
	if !trigmatch.AttacksMatches(boardOf(e), trig, scourge, evt(0)) {
		t.Fatal("a dead player's larger total must not veto the trigger")
	}
	// A dead DEFENDER never matches.
	if trigmatch.AttacksMatches(boardOf(e), trig, scourge, evt(2)) {
		t.Fatal("a dead defender must not match")
	}
	// The condition on a DIFFERENT mode's line is not this matcher's burden;
	// but the condition absent must stay vacuous (no regression to the
	// plain Attacks trigger).
	trigPlain := cards.Trigger{Mode: "Attacks", Params: map[string]string{"ValidCard": "Creature.Self"}}
	setLives(10, 20, 25)
	if !trigmatch.AttacksMatches(boardOf(e), trigPlain, scourge, evt(0)) {
		t.Fatal("plain Attacks trigger (no Condition$) must still match a low-life defender")
	}
}
