package botpolicy

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
)

// gateDecision builds the KPriority decision the engine poses at a main
// phase with an empty hand: the Whirler Rogue activation (kind "ability")
// and a "pass" option. This is the exact shape the bot faces before costs
// are paid, so the test pins the pre-activation decline at the decision the
// policy actually answers.
func gateDecision(b Board, statics []string) (int, *decision.Decision) {
	// IsMain is required: the ability block is a sorcery-speed main-phase
	// action, and a zero Board reports false.
	b.IsMain = true
	d := decision.Decision{Seq: 1, Player: 0, Kind: decision.KPriority, Min: 1, Max: 1,
		Options: []decision.Option{
			{Index: 0, Kind: "ability", Obj: 300, Label: "Whirler Rogue: Target creature can't be blocked this turn.", GrantStatics: statics},
			{Index: 1, Kind: "pass"},
		}}
	in := Decide(b, &d, rng(1))
	return in.Choices[0], &d
}

// TestBoonActivationDeclinedWithoutOwnCreature is the A1c regression for
// fb-20260927T153930Z: with Whirler Rogue and two untapped artifacts on the
// board but NO own creature (the enemy has the only creature), the bot must
// pass instead of activating, because the grant could then only land on an
// opponent's creature. Before the gate the ability scored 980 and was
// activated, and the follow-up target ask was answered with an opponent.
func TestBoonActivationDeclinedWithoutOwnCreature(t *testing.T) {
	b := boardOf(def(1, 2, 2))
	pick, d := gateDecision(b, []string{"CantBlockBy"})
	// Precondition: the option really carries the polarity signal the gate
	// reads; a nil/empty GrantStatics would make this test vacuous.
	if !boonStaticsOnly(d.Options[0].GrantStatics) {
		t.Fatalf("precondition: ability option carries no boon statics: %v", d.Options[0].GrantStatics)
	}
	// Precondition: the activation really was offered, so a "pass" answer is
	// the policy's choice and not the engine withholding the option.
	if d.Options[0].Kind != "ability" {
		t.Fatalf("precondition: option 0 is %q, want \"ability\"", d.Options[0].Kind)
	}
	if pick != 1 {
		t.Fatalf("bot picked option %d (%+v), want pass (1): it must not activate a boon grant with no own creature",
			pick, d.Options[pick])
	}
}

// TestBoonActivationAcceptedWithOwnCreature is the positive half: the same
// board with an own creature besides the source still activates the grant --
// the follow-up target ask leads with the own board (R1B), so the activation
// is sensible.
func TestBoonActivationAcceptedWithOwnCreature(t *testing.T) {
	b := boardOf(def(1, 2, 2), atk(1, 5, 5))
	pick, d := gateDecision(b, []string{"CantBlockBy"})
	// Precondition: the census really sees an own creature besides the
	// option's source (obj 300), so an activation is not vacuously accepted.
	if !b.hasOwnCreature(0) {
		t.Fatal("precondition: board unexpectedly has no own creature")
	}
	if _, ok := b.Creatures.Lookup(300); ok {
		t.Fatal("precondition: board unexpectedly offers the source as a creature")
	}
	if pick != 0 {
		t.Fatalf("bot picked option %d (%+v), want the ability (0): it should activate a boon grant with an own creature to receive it",
			pick, d.Options[pick])
	}
}

// TestBoonActivationDeclinedWhenOnlyTheSourceIsOwn is the census-scope test
// for the reported board shape: Whirler Rogue is itself a creature, so a
// census that includes the source would find an "own creature" and the gate
// would never fire. With the source excluded, a board whose only own
// creature IS the source still declines -- two artifacts must not be spent
// to make Whirler Rogue itself unblockable when no attack plan exists.
func TestBoonActivationDeclinedWhenOnlyTheSourceIsOwn(t *testing.T) {
	b := boardOf(def(1, 2, 2))
	b.Creatures.Set(300, Creature{Power: 2, Toughness: 2, Controller: 0}) // the source, Whirler Rogue
	pick, d := gateDecision(b, []string{"CantBlockBy"})
	// Precondition: the inclusive census WOULD have seen the source, so the
	// decline below is attributable to the exclusion, not to an empty board.
	if !b.hasOwnCreature(0) {
		t.Fatal("precondition: the inclusive census finds no own creature")
	}
	if pick != 1 {
		t.Fatalf("bot picked option %d (%+v), want pass (1): granting evasion to the source itself is no sensible use of the activation",
			pick, d.Options[pick])
	}
}

// TestNonBoonGrantNotDeclinedByTheGate guards the polarity scope: an
// activation whose statics are NOT all readable boons (an unreadable or
// harmful grant) stays on the ordinary scoring path, so the A1c decline
// does not suppress abilities it cannot classify.
func TestNonBoonGrantNotDeclinedByTheGate(t *testing.T) {
	b := boardOf(def(1, 2, 2)) // no own creature: the gate would fire if it read polarity wrong.
	pick, d := gateDecision(b, []string{"CantRegenerate"})
	// Precondition: the statics really are outside the boon set, so a pass
	// here would be the gate overreaching.
	if boonStaticsOnly(d.Options[0].GrantStatics) {
		t.Fatal("precondition: CantRegenerate unexpectedly classifies as a boon")
	}
	if pick != 0 {
		t.Fatalf("bot passed on a non-boon activation (pick %d), want the ability (0): the gate must only decline readable boons",
			pick)
	}
}
