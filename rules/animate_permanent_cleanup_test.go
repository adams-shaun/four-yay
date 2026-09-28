package rules

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// The effAnimate lifetime pin (review r2 MAJOR finding): the LType grant
// honours Duration$/Permanent while the LPT (SetPower/SetToughness) grant
// hardcoded UntilEOT, so a Duration$ Permanent land animation kept its
// types through end-of-turn cleanup but lost its P/T — an untransformed-
// basis 0/0 creature the CR 704.5f state-based action then destroys, i.e.
// the player LOSES the land the animation was supposed to keep. The rule
// the fix states: a permanent animation is WHOLLY permanent — the P/T
// grant carries the same Duration/Permanent/UntilEOT lifetime as the
// type/colour/keyword/ability grants beside it. Stalking Stones is the
// real corpus carrier: {6}: "becomes a 3/3 Elemental artifact creature
// that's still a land. (This effect lasts indefinitely.)" — Duration$
// Permanent with Power$ 3 | Toughness$ 3, self-targeted, no tap cost.

// TestKarnAnimateUntilYourNextTurnSurvivesTheOpponentsTurn pins the real
// Karn, the Great Creator +1 and a real noncreature artifact. UntilYourNextTurn
// ends when Karn's next turn starts, so the animated artifact must remain a
// creature throughout the opponent's turn but revert before Karn's next main.
func TestKarnAnimateUntilYourNextTurnSurvivesTheOpponentsTurn(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := corpusEngine(t, reg, []*cards.Card{
		lookup(t, reg, "Karn, the Great Creator"), lookup(t, reg, "Sol Ring"),
	}, []*cards.Card{})
	karn := moveByName(t, e, 0, "Karn, the Great Creator", state.ZBattlefield)
	ring := moveByName(t, e, 0, "Sol Ring", state.ZBattlefield)
	if e.G.Obj(karn).Counter("LOYALTY") <= 0 || e.G.Obj(ring).Zone != state.ZBattlefield {
		t.Fatalf("precondition: Karn=%+v Sol Ring=%+v", e.G.Obj(karn), e.G.Obj(ring))
	}
	if e.IsCreature(ring) {
		t.Fatal("precondition: Sol Ring is already a creature")
	}
	e.pending = nil
	e.priorityRound()

	submitChoices(t, e, abilityOption(t, e, karn, 0).Index)
	submitTarget(t, e, ring)
	passUntilStackEmpty(t, e, 40)
	if !e.IsCreature(ring) {
		t.Fatal("Karn's +1 did not animate Sol Ring")
	}

	// Turn 2 is the opponent's turn; the effect must still be active there.
	driveToStep(t, e, 2, 1, state.StepMain1)
	if !e.IsCreature(ring) {
		t.Fatal("Karn's animation expired before the opponent's turn")
	}

	// The next turn is Karn's boundary: the animation has ended at its start.
	driveToStep(t, e, 3, 0, state.StepMain1)
	if e.IsCreature(ring) {
		t.Fatal("Karn's animation survived into Karn's next turn")
	}
}

func TestAnimatePermanentDurationKeepsPTThroughCleanup(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := corpusEngine(t, reg, []*cards.Card{lookup(t, reg, "Stalking Stones")}, []*cards.Card{})
	id := moveByName(t, e, 0, "Stalking Stones", state.ZBattlefield)

	addMana(t, e, 0, "CCCCCC")
	submitChoices(t, e, animateAbilityOption(t, e, id).Index)
	settleActivation(t, e)

	d := e.Derived(id)
	if d.Power != 3 || d.Toughness != 3 {
		t.Fatalf("animated Stalking Stones = %d/%d, want 3/3", d.Power, d.Toughness)
	}
	if !e.IsCreature(id) {
		t.Fatal("animated Stalking Stones is not a creature")
	}

	// End-of-turn cleanup must strip NEITHER half of the animation.
	e.EndOfTurnCleanup()
	d = e.Derived(id)
	if d.Power != 3 || d.Toughness != 3 {
		t.Fatalf("after cleanup: %d/%d, want 3/3 — the Duration$ Permanent P/T grant expired (pre-fix behaviour: types kept, P/T stripped to a 0/0)", d.Power, d.Toughness)
	}
	if !slices.Contains(d.Types, "Elemental") || !slices.Contains(d.Types, "Creature") || !slices.Contains(d.Types, "Land") {
		t.Fatalf("after cleanup types = %v, want Elemental+Creature+Land", d.Types)
	}
	if !e.IsCreature(id) {
		t.Fatal("after cleanup the animated land is not a creature")
	}
	// The CR 704.5f toughness-0 SBA must have nothing to do: the land
	// survives with its permanent 3/3, not as a doomed 0/0.
	e.checkStateBased()
	if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
		t.Fatal("the land was destroyed after cleanup — the permanent animation's P/T did not survive")
	}
}
