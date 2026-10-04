package rules

// Kernel-era restorations of the tests W3 removed from parent_target_resume_test.go: the
// same behaviour driven through the resolution kernel (the only ask path).

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

// TestParentTargetLinkRecordClonesWithSuspension proves the ride is
// DEEP-copied, not aliased: a Clone taken while the discard ask is pending
// must own the parent-link record, and both engines must still resume to the
// nearest targeting link when the SAME answer is served to each.
func TestParentTargetLinkRecordClonesWithSuspensionKernel(t *testing.T) {
	t.Parallel()
	e, _, _ := newFixtureDeck(t, 7103, parentTargetBetweenScript(),
		ptResumeBearSrc, ptResumeAngelSrc)
	bear := moveByName(t, e, 0, "ParentLink Bear", state.ZBattlefield)
	angel := moveByName(t, e, 0, "ParentLink Angel", state.ZBattlefield)
	spell := fixtureInHand(t, e, "Parent Link Between")
	addMana(t, e, 0, "R")
	submitChoices(t, e, castOptionFor(t, e, spell).Index)
	d := advanceToDiscardAsk(t, e, bear, angel)

	c := e.Clone()
	if p := c.Pending(); p == nil || p.Seq != d.Seq || p.Kind != d.Kind {
		t.Fatalf("clone lost the pending discard ask: %+v vs %+v", p, d)
	}

	// The same discard answer must drive both engines to the link target.
	answerDiscard(t, e, d)
	answerDiscard(t, c, d)
	drainParentLink(t, e)
	drainParentLink(t, c)
	for name, eng := range map[string]*Engine{"original": e, "clone": c} {
		if !eng.HasKeyword(angel, "Flying") {
			t.Fatalf("%s: ParentLink Angel lacks Flying after the suspension", name)
		}
		if eng.HasKeyword(bear, "Flying") {
			t.Fatalf("%s: ParentLink Bear gained Flying (fell back to the root's targets)", name)
		}
	}
}
