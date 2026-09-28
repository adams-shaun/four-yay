package rules

// DB$ Clone Choices$ <filter> end-to-end pins (ticket approx-clone-choices).
//
// The row this file closes said Choices$ "takes the deterministic first-eligible
// object under a Note (R-9)". These tests pin the replacement: a real
// mid-resolution KChoose over the eligible pool, whose answer the effect uses
// (so the SECOND creature of two is copied, not the first), and whose answer
// survives a later Optional$ may-copy ask in the same walk (the
// Decision.ResumeClonePick rider) instead of being re-posed.
//
// Every fixture is an inline Forge script (never a .cards/ file, per the
// licensing rule); the engine path under test is the real primitive.

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// clonePickFixture is the clone carrier: an artifact whose {1} ability
// becomes a copy of a creature chosen with Choices$. It is not a creature, so
// the Choices$ pool is exactly the creatures the test places beside it.
const clonePickMimic = "Name:Fixture Picky Mimic\nManaCost:2\nTypes:Artifact\n" +
	"A:AB$ Clone | Cost$ 1 | Choices$ Creature | ChoiceTitle$ Choose a creature to copy" +
	" | SpellDescription$ becomes a copy of a creature.\nOracle:x\n"

// clonePickOptionalMimic adds Optional$ True, the Sarkhan/Deepfathom-Echo
// "you may have it become a copy" shape: after the Choices$ pick the walk
// poses a second, yes/no ask in the SAME resolution.
const clonePickOptionalMimic = "Name:Fixture Dubious Mimic\nManaCost:2\nTypes:Artifact\n" +
	"A:AB$ Clone | Cost$ 1 | Choices$ Creature | Optional$ True" +
	" | ChoiceTitle$ Choose a creature to copy" +
	" | SpellDescription$ may become a copy of a creature.\nOracle:x\n"

const clonePickAlpha = "Name:Fixture Alpha Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"
const clonePickBeta = "Name:Fixture Beta Bear\nManaCost:1 R\nTypes:Creature Bear\nPT:3/3\nOracle:x\n"

// cloneCreatureChoice reports whether d is the Choices$ pick over the two
// placed creatures (a KChoose whose options each name a permanent). It is how
// these tests tell the real ask from a later yes/no or target ask.
func cloneCreatureChoice(d *decision.Decision) bool {
	if d == nil || d.Kind != decision.KChoose || len(d.Options) < 2 {
		return false
	}
	for _, o := range d.Options {
		if o.Kind != "permanent" || o.Obj == 0 {
			return false
		}
	}
	return true
}

// cloneOptionFor returns the option index naming obj, or -1.
func cloneOptionFor(d *decision.Decision, obj state.ObjID) int {
	for _, o := range d.Options {
		if o.Obj == obj {
			return o.Index
		}
	}
	return -1
}

// TestCloneChoicesPosesRealAsk is the core pin: Choices$ poses a real ask
// whose TWO creature options are the two placed bears, and the copy follows
// the answered pick (Beta, the second), not the deterministic-first Alpha.
func TestCloneChoicesPosesRealAsk(t *testing.T) {
	t.Parallel()
	e, cfg, _ := newFixtureDeck(t, 930, clonePickMimic, clonePickAlpha, clonePickBeta)

	mimic := searchMoveByName(t, e, "Fixture Picky Mimic", state.ZBattlefield)
	alpha := searchMoveByName(t, e, "Fixture Alpha Bear", state.ZBattlefield)
	beta := searchMoveByName(t, e, "Fixture Beta Bear", state.ZBattlefield)

	// Preconditions the assertions depend on: the mimic is untouched (not a
	// creature), and the two bears are distinct objects with distinct names.
	if e.IsCreature(mimic) {
		t.Fatal("precondition: the mimic is already a creature before the copy")
	}
	if alpha == 0 || beta == 0 || alpha == beta {
		t.Fatalf("precondition: two distinct bears required, got %d and %d", alpha, beta)
	}
	if a := e.G.Obj(alpha).Face().Name; a != "Fixture Alpha Bear" {
		t.Fatalf("precondition: alpha face %q", a)
	}
	if b := e.G.Obj(beta).Face().Name; b != "Fixture Beta Bear" {
		t.Fatalf("precondition: beta face %q", b)
	}

	addMana(t, e, 0, "C")
	submitChoices(t, e, abilityOption(t, e, mimic, 0).Index)

	d := passUntilNonPriority(t, e, 40)
	if !cloneCreatureChoice(d) {
		t.Fatalf("expected a real Choices$ KChoose over the creatures, got %+v", d)
	}
	if got := len(d.Options); got != 2 {
		t.Fatalf("Choices$ offered %d options, want exactly the 2 placed creatures: %+v", got, d.Options)
	}
	betaIdx := cloneOptionFor(d, beta)
	alphaIdx := cloneOptionFor(d, alpha)
	if betaIdx < 0 || alphaIdx < 0 {
		t.Fatalf("Choices$ options %+v do not cover both bears (%d, %d)", d.Options, alpha, beta)
	}

	// The deterministic-first stand-in would have taken Alpha; answer Beta.
	submitChoices(t, e, betaIdx)
	passUntilStackEmpty(t, e, 40)

	f := e.G.Obj(mimic).Face()
	if f == nil || f.Name != "Fixture Beta Bear" {
		t.Fatalf("copy name %v, want the ANSWERED Beta Bear (the bug copied the first-eligible Alpha)", f)
	}
	if !e.IsCreature(mimic) {
		t.Fatal("the answered copy is not a creature")
	}
	if d := e.Derived(mimic); d.Power != 3 || d.Toughness != 3 {
		t.Fatalf("copy P/T %d/%d, want Beta's 3/3", d.Power, d.Toughness)
	}
	// And it is demonstrably NOT the first-eligible object.
	if f.Name == "Fixture Alpha Bear" {
		t.Fatal("copy took Alpha, the deterministic first-eligible stand-in")
	}
	replayCheck(t, e, cfg)
}

// TestCloneChoicesAnswerSurvivesOptionalAsk pins the resume carry: with BOTH
// Choices$ and Optional$ True, answering the Choices$ pick and then the
// Optional$ yes must complete the copy without posing a second Choices$ ask.
// Without Decision.ResumeClonePick the Optional$ re-entry finds the pick
// consumed and re-poses the Choices$ ask (a wedge), so this test is the
// regression pin for the carry.
func TestCloneChoicesAnswerSurvivesOptionalAsk(t *testing.T) {
	t.Parallel()
	e, cfg, _ := newFixtureDeck(t, 931, clonePickOptionalMimic, clonePickAlpha, clonePickBeta)

	mimic := searchMoveByName(t, e, "Fixture Dubious Mimic", state.ZBattlefield)
	alpha := searchMoveByName(t, e, "Fixture Alpha Bear", state.ZBattlefield)
	beta := searchMoveByName(t, e, "Fixture Beta Bear", state.ZBattlefield)
	if e.IsCreature(mimic) {
		t.Fatal("precondition: the mimic is already a creature before the copy")
	}
	if alpha == 0 || beta == 0 || alpha == beta {
		t.Fatalf("precondition: two distinct bears required, got %d and %d", alpha, beta)
	}

	addMana(t, e, 0, "C")
	submitChoices(t, e, abilityOption(t, e, mimic, 0).Index)

	d := passUntilNonPriority(t, e, 40)
	if !cloneCreatureChoice(d) {
		t.Fatalf("expected the Choices$ KChoose first, got %+v", d)
	}
	betaIdx := cloneOptionFor(d, beta)
	if betaIdx < 0 {
		t.Fatalf("Choices$ options %+v do not include Beta %d", d.Options, beta)
	}
	submitChoices(t, e, betaIdx)

	// The second ask in the same walk is the Optional$ yes/no, NOT a second
	// Choices$ pick.
	d = passUntilNonPriority(t, e, 40)
	if cloneCreatureChoice(d) {
		t.Fatalf("the Choices$ ask was re-posed before the Optional$ ask: %+v", d)
	}
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "clone" {
		t.Fatalf("expected the Optional$ clone yes/no ask, got %+v", d)
	}
	yesIdx := -1
	for _, o := range d.Options {
		if o.Kind == "yes" {
			yesIdx = o.Index
		}
	}
	if yesIdx < 0 {
		t.Fatalf("Optional$ ask offers no yes option: %+v", d.Options)
	}
	submitChoices(t, e, yesIdx)

	// The answered re-entry must consume the carried pick, not re-ask. If the
	// carry were missing this is exactly where a second Choices$ KChoose would
	// surface, so assert on it directly before draining.
	if d := e.Pending(); cloneCreatureChoice(d) {
		t.Fatalf("Choices$ ask re-posed after the Optional$ answer (ResumeClonePick not carried): %+v", d)
	}
	passUntilStackEmpty(t, e, 40)

	f := e.G.Obj(mimic).Face()
	if f == nil || f.Name != "Fixture Beta Bear" {
		t.Fatalf("copy name %v, want the answered Beta Bear", f)
	}
	replayCheck(t, e, cfg)
}
