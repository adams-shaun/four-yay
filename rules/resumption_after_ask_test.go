package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestResumptionRunsOuterContinuationAfterNestedAsk is the F19 resumption
// coverage for the one shape the suspension half's tests do not reach: a
// mid-resolution ask nested INSIDE a Charm mode, where the Charm carries its
// OWN SubAbility$ continuation AFTER the mode.
//
// The suspension half (a head-of-chain Discard/Charm ask suspending the
// resolution and posing a KModes decision, with a chained SubAbility$ running
// once the answer lands) is covered by TestThoughtseizeLosesExactlyTwoLife and
// TestCharmSubAbilityRunsOnce. Neither reaches the nested shape: there, the
// inner ask's resume point OVERWRITES the outer one, so the engine re-enters
// at the inner SA and never walks the Charm's own continuation.
//
// This card resolves to: choose a mode (DoDiscard), the mode itself discards
// a card via a mid-resolution TgtChoose ask, the mode's own chained
// SubAbility$ (DBSub, lose 10) must then fire once, and — the part on trial —
// the Charm's own SubAbility$ (DBLife, lose 1) must fire once AFTER both, with
// the discard answer already applied (the chosen card visible in the
// graveyard). Correct resolution: the caster loses 11 total over two distinct
// lose-life events.
func TestResumptionRunsOuterContinuationAfterNestedAsk(t *testing.T) {
	t.Parallel()
	charm := "Name:PiN\nManaCost:R\nTypes:Instant\n" +
		"A:SP$ Charm | Choices$ DoDiscard,DoGain | SubAbility$ DBLife\n" +
		"SVar:DoDiscard:DB$ Discard | Defined$ You | Mode$ TgtChoose | NumCards$ 1 | SubAbility$ DBSub\n" +
		"SVar:DoGain:DB$ GainLife | Defined$ You | LifeAmount$ 5 | SpellDescription$ Gain 5 life\n" +
		"SVar:DBLife:DB$ LoseLife | Defined$ You | LifeAmount$ 1\n" +
		"SVar:DBSub:DB$ LoseLife | Defined$ You | LifeAmount$ 10\nOracle:x\n"
	e, _, id := newFixtureDeck(t, 91, charm)
	addMana(t, e, 0, "R")
	life := e.G.Players[0].Life

	// Pass 1: the Charm itself asks for a mode. Pick DoDiscard (index 0).
	d := castFixture(t, e, id, -1)
	if d == nil || d.Kind != decision.KModes {
		t.Fatalf("expected the Charm mode KModes ask, got %+v", d)
	}
	submitChoices(t, e, 0)
	d = passUntilNonPriority(t, e, 20)

	// Pass 2: the chosen mode (Discard) is itself a mid-resolution ask. Answer
	// it, discarding the first offered card — deliberately a chosen card, so
	// we can prove the answer's effect (that exact discard) is applied before
	// any continuation runs.
	d = e.Pending()
	if d == nil || d.Kind != decision.KModes {
		t.Fatalf("expected the Discard mode's KModes ask, got %+v", d)
	}
	chosen := d.Options[0].Obj
	submitChoices(t, e, d.Options[0].Index)
	passUntilStackEmpty(t, e, 20)

	// The answer's effect is visible: the chosen card reached the graveyard.
	if z := e.G.Obj(chosen).Zone; z != state.ZGraveyard {
		t.Fatalf("chosen discard target zone = %s, want Graveyard (answer not applied before the continuation)", z)
	}

	// The inner continuation (the mode's own SubAbility$ DBSub) plus the outer
	// continuation (the Charm's own SubAbility$ DBLife) must each fire EXACTLY
	// once, after the answer. Two distinct lose-life events, 11 life total.
	if got := countLoseLife(e, 0); got != 2 {
		t.Fatalf("lose-life events = %d, want exactly 2 (inner DBSub -10 AND outer DBLife -1); the outer continuation fires %d times on the resume", got, got-1)
	}
	if want := life - 11; e.G.Players[0].Life != want {
		t.Fatalf("caster life = %d, want %d (inner DBSub -10 and outer DBLife -1 each exactly once)", e.G.Players[0].Life, want)
	}
	if !hasEventKind(e, events.ModeChosen) {
		t.Fatal("no ModeChosen event recorded the nested answer")
	}
}

// TestResumptionRunsEveryContinuationAtDepthThree is the depth-3 case that
// separates a real chain of resume points from a one-deep patch: the ask is
// nested TWO levels below the outermost continuation. A single-slot resume
// point and a one-deep fix both pass a depth-2 test while still dropping the
// middle and outer continuations here, so this is what pins the fix as a
// chain, not a patch.
//
// The card is a Charm whose chosen mode is a Repeat, whose Repeated
// sub-ability is the Discard (the asking primitive), with a continuation
// after EACH of the three levels:
//
//	charm1SA: Charm | Choices DoRepeat,DoGain | SubAbility$ Out       (level-0 cont)
//	DoRepeat: SP$ Repeat | RepeatSubAbility$ DoDiscard | SubAbility$ Mid (level-1 cont)
//	DoDiscard: DB$ Discard | Mode$ TgtChoose | SubAbility$ Inn          (asking, level-2 cont)
//
// Two mid-resolution KModes asks fire in sequence — the Charm mode, then the
// discard TgtChoose — and after the discard answer is applied the inner
// (Inn), middle (Mid) and outer (Out) continuations must each run exactly
// once, in that order: 1 + 2 + 3 life lost over three distinct
// lose-life events. The chosen card must already be in the graveyard when any
// continuation fires, proving the answer's effect is applied first.
//
// Note this shape is deliberately a Repeat at the middle level, not a second
// Charm: effCharm resolves a chosen mode by handing it the SAME Ctx (whose
// Modes the parent already set), so a Charm invoked AS a mode inherits that
// Modes value and re-resolves itself endlessly. Repeat has no such coupling,
// so it is the one expressible two-level carrier.
func TestResumptionRunsEveryContinuationAtDepthThree(t *testing.T) {
	t.Parallel()
	charm := "Name:PiN3\nManaCost:R\nTypes:Instant\n" +
		"A:SP$ Charm | Choices$ DoRepeat,DoGain | SubAbility$ Out\n" +
		"SVar:DoRepeat:SP$ Repeat | RepeatSubAbility$ DoDiscard | RepeatNum$ 1 | SubAbility$ Mid\n" +
		"SVar:DoDiscard:DB$ Discard | Defined$ You | Mode$ TgtChoose | NumCards$ 1 | SubAbility$ Inn\n" +
		"SVar:DoGain:DB$ GainLife | Defined$ You | LifeAmount$ 5\n" +
		"SVar:Out:DB$ LoseLife | Defined$ You | LifeAmount$ 3\n" +
		"SVar:Mid:DB$ LoseLife | Defined$ You | LifeAmount$ 2\n" +
		"SVar:Inn:DB$ LoseLife | Defined$ You | LifeAmount$ 1\nOracle:x\n"
	e, _, id := newFixtureDeck(t, 92, charm)
	addMana(t, e, 0, "R")
	life := e.G.Players[0].Life

	// Pass 1: the outer Charm asks for a mode. Pick DoRepeat (index 0).
	d := castFixture(t, e, id, -1)
	if d == nil || d.Kind != decision.KModes {
		t.Fatalf("expected the Charm mode KModes ask, got %+v", d)
	}
	submitChoices(t, e, 0)
	d = passUntilNonPriority(t, e, 20)

	// Pass 2: the chosen mode (Repeat) runs its Repeated Discard, which is
	// itself a mid-resolution ask. Answer it, discarding the first offered
	// card.
	d = e.Pending()
	if d == nil || d.Kind != decision.KModes {
		t.Fatalf("expected the Discard mode's KModes ask, got %+v", d)
	}
	chosen := d.Options[0].Obj
	submitChoices(t, e, d.Options[0].Index)
	passUntilStackEmpty(t, e, 20)

	// The answer's effect is visible: the chosen card reached the graveyard
	// before any continuation fired.
	if z := e.G.Obj(chosen).Zone; z != state.ZGraveyard {
		t.Fatalf("chosen discard target zone = %s, want Graveyard (answer not applied before the continuation)", z)
	}

	// Inn, Mid and Out must each fire EXACTLY once, after the answer: three
	// distinct lose-life events, 6 life total (the caster survives, so the
	// discarded card stays in the graveyard rather than ceasing when the game
	// ends).
	if got := countLoseLife(e, 0); got != 3 {
		t.Fatalf("lose-life events = %d, want exactly 3 (Inn -1, Mid -2, Out -3); continuations fired on the resume = %d", got, got)
	}
	if want := life - 6; e.G.Players[0].Life != want {
		t.Fatalf("caster life = %d, want %d (Inn -1, Mid -2 and Out -3 each exactly once)", e.G.Players[0].Life, want)
	}
}
