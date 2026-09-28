package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// TestEffectContinuousExpiresAtEndOfNextTurn pins the turn-boundary lifetime
// of a Duration$ UntilTheEndOfYourNextTurn effect -- fix F1, and the largest
// non-Permanent Duration$ population (105 raw lines / 105 compiled SAs). The
// restriction must survive the CURRENT turn's cleanup AND the opponent's
// turn, stay live through the controller's NEXT turn, and only then expire:
// it is neither a premature end-of-this-turn expiry (UntilEOT, the shape the
// instant-source branch used to give it a turn early) nor a never-expiring
// source-leaves effect. Driving the real turn/card flow and observing the
// registry's own expiry turn is the boundary measurement.
//
// Like its end-of-turn sibling (continuous_effect_lifetime_test.go) this
// reads the engine's own restriction registry -- restrictionBlocksTarget and
// the ContinuousEffect.UntilTurn field, both new on this branch -- so it is a
// "passes here" boundary pin rather than a base-compile proof. The card-flow
// fail-on-base proof that the restriction is real at all is the Vines test
// (same Duration$ shape, Permanent); the new thing here is the boundary.
func TestEffectContinuousExpiresAtEndOfNextTurn(t *testing.T) {
	t.Parallel()
	guard := card(t, "Name:Guard\nManaCost:G\nTypes:Instant\n"+
		"A:SP$ Pump | ValidTgts$ Creature | NumAtt$ +2 | NumDef$ +2 | SubAbility$ DBEffect\n"+
		"SVar:DBEffect:DB$ Effect | Defined$ Targeted | Duration$ UntilTheEndOfYourNextTurn | StaticAbilities$ Guard | RememberObjects$ Targeted\n"+
		"SVar:Guard:Mode$ CantTarget | ValidTarget$ Card.IsRemembered | Activator$ Player.Opponent\nOracle:x\n")
	e := handEngine(t, guard)
	e.G.Players[0].Pool[state.MG] = 1
	// Seat 0's own creature, so the pump and the CantTarget restriction both
	// land on a permanent that stays on the battlefield through the drive.
	creature := onBoard(t, e, 0, "Name:Elf\nManaCost:G\nTypes:Creature Elf\nPT:2/2\nOracle:x\n")

	e.askPriority(0)
	castFirst(t, e, "cast")
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected a target decision for Guard, got %+v", d)
	}
	idx := -1
	for _, o := range d.Options {
		if o.Obj == creature {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("creature not offered as Guard target: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	passUntilStackEmpty(t, e, 8)
	if len(e.G.Stack) != 0 {
		t.Fatalf("Guard did not resolve: stack %v", e.G.Stack)
	}

	// The registered restriction must carry the expiry turn. This is a 2-seat
	// game with seat 0 active on turn 1 (the current turn), so seat 0's NEXT
	// turn is turn 3 -- exactly the boundary the effect must survive to, and
	// the measurement the drive below confirms behaviorally.
	found := false
	for _, ce := range e.continuous {
		if ce.Restriction == "CantTarget" {
			found = true
			if ce.UntilTurn != 3 {
				t.Fatalf("next-turn effect expiry = turn %d, want 3 (seat 0's next turn)", ce.UntilTurn)
			}
			if ce.UntilEOT {
				t.Fatal("a next-turn duration must not be marked UntilEOT (it would expire a turn early)")
			}
		}
	}
	if !found {
		t.Fatal("no CantTarget restriction was registered for the next-turn effect")
	}

	// On the current turn (turn 1) the restriction bites the opponent.
	if !e.restrictionBlocksTarget(creature, 1) {
		t.Fatal("restriction should bite the opponent on the casting turn")
	}
	// Drive through turn 1's and turn 2's cleanups into seat 0's next turn
	// (turn 3). Crossing two cleanups without dropping the effect is exactly
	// the property an UntilEOT (or source-leaves) lifetime would fail: the
	// effect must survive the opponent's turn and be live on my next one.
	driveToStepAll(t, e, 3, 0, state.StepMain1)
	if !e.restrictionBlocksTarget(creature, 1) {
		t.Fatal("restriction did not survive to the controller's next turn; it expired a turn early")
	}
	// Drive through turn 3's cleanup into turn 4: the boundary turn's cleanup
	// must drop it, so the restriction no longer bites after my next turn.
	driveToStepAll(t, e, 4, 1, state.StepMain1)
	if e.restrictionBlocksTarget(creature, 1) {
		t.Fatal("restriction survived past the end of the controller's next turn; it never expires")
	}
}
