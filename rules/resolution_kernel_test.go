package rules

// Kernel-era restorations of the tests W3 removed from resolution_test.go: the
// same behaviour driven through the resolution kernel (the only ask path).

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
)

// TestSuspendedResolutionSurvivesAClone pins the clone-safety requirement
// that the whole resume design is built on: the resume state is plain
// value/pointer data (kind/obj plus a shared-immutable *cards.SA), never a
// closure, so an Engine.Clone taken while a mid-resolution KModes decision
// is pending sees the same suspension, and answering both engines
// identically produces the same chain head. A closure captured over the
// original engine would fail exactly here (the clone would resume into
// nothing), which is the whole reason the field is structured this way.
func TestSuspendedResolutionSurvivesACloneKernel(t *testing.T) {
	t.Parallel()
	charm := "Name:PiC\nManaCost:R\nTypes:Instant\nA:SP$ Charm | Choices$ DoDiscard,DoGain\n" +
		"SVar:DoDiscard:DB$ Discard | Defined$ You | Mode$ TgtChoose | NumCards$ 1 | SpellDescription$ Discard a card\n" +
		"SVar:DoGain:DB$ GainLife | Defined$ You | LifeAmount$ 5 | SpellDescription$ Gain 5 life\nOracle:x\n"
	e, cfg, id := newFixtureDeck(t, 95, charm)
	addMana(t, e, 0, "R")
	d := castFixture(t, e, id, -1)
	if d == nil || d.Kind != decision.KModes {
		t.Fatalf("expected the cast-time KModes announcement, got %+v", d)
	}
	submitChoices(t, e, 0)
	d = passUntilNonPriority(t, e, 20)
	if d == nil || d.Kind != decision.KModes {
		t.Fatalf("expected the mid-resolution Discard KModes decision, got %+v", d)
	}
	c := e.Clone()
	if p := c.Pending(); p == nil || p.Kind != decision.KModes {
		t.Fatalf("clone lost the suspended decision: %+v", p)
	}
	// Answer both engines identically (same choice, then the same drain),
	// and require the same chain head — the cloned suspension must resume
	// into the same continuation and re-derive the same tail.
	submitEach := func(x *Engine, choices ...int) {
		d := x.Pending()
		if err := x.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: choices}); err != nil {
			t.Fatalf("submit %v: %v", choices, err)
		}
	}
	submitEach(e, d.Options[0].Index)
	drainToEnd(t, e, 30)
	submitEach(c, d.Options[0].Index)
	drainToEnd(t, c, 30)
	if e.L.Head() != c.L.Head() {
		t.Fatalf("clone diverged: chain %s vs %s", e.L.Head(), c.L.Head())
	}
	replayCheck(t, c, cfg)
}
