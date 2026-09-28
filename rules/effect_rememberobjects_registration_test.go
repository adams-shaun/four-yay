package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

// TestEffectRememberObjectsRegistration pins the registration half of the
// RememberObjects$ selector fix: an Effect that captures a Valid <filter>
// set must register its continuous grant against exactly those remembered
// objects, through the same Affected$ Card.IsRemembered machinery the
// Targeted case already used. Before the fix the selector was unresolved, so
// the registered grant matched nobody.
//
// The board deliberately puts an opponent-controlled creature (the captured
// set) beside the controller's own creature (the control), and the test
// asserts BOTH the positive and the negative, with the usual preconditions
// (both on the battlefield, neither already carrying the granted keyword).
func TestEffectRememberObjectsRegistration(t *testing.T) {
	t.Parallel()
	grant := card(t, "Name:MarkOpponents\nManaCost:U\nTypes:Sorcery\n"+
		"A:SP$ Effect | StaticAbilities$ STMark | RememberObjects$ Valid Creature.OppCtrl\n"+
		"SVar:STMark:Mode$ Continuous | Affected$ Card.IsRemembered | AddKeyword$ Flying\n"+
		"Oracle:x\n")
	e := handEngine(t, grant)
	e.G.Players[0].Pool[state.MU] = 1
	theirs := onBoard(t, e, 1, "Name:TheirBear\nManaCost:G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	ours := onBoard(t, e, 0, "Name:OurBear\nManaCost:G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")

	// Preconditions: both creatures are on the battlefield, the two compared
	// objects differ, and neither printed face already has the granted
	// keyword -- a vacuous board must fail here rather than pass silently.
	for _, id := range []state.ObjID{theirs, ours} {
		if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
			t.Fatalf("precondition: creature %d not on the battlefield: %+v", id, o)
		}
		if e.G.Obj(id).Face().HasKeyword("Flying") {
			t.Fatalf("precondition: creature %d already has Flying", id)
		}
	}
	if theirs == ours {
		t.Fatal("precondition: the two creatures must differ")
	}
	if e.HasKeyword(theirs, "Flying") || e.HasKeyword(ours, "Flying") {
		t.Fatal("precondition: a creature already has Flying before the grant")
	}

	e.askPriority(0)
	castFirst(t, e, "cast")
	passUntilStackEmpty(t, e, 8)
	if len(e.G.Stack) != 0 {
		t.Fatalf("MarkOpponents did not resolve: stack %v", e.G.Stack)
	}

	// The feature's handler must actually have run: the general Continuous
	// grant must not have fallen to the unimplemented Note (which would make
	// a "nothing happens" assertion pass with the registration reverted).
	if notes := effectContinuousUnimplementedNotes(e); len(notes) != 0 {
		t.Fatalf("the Continuous grant fell to the unimplemented Note: %v", notes)
	}
	if !e.HasKeyword(theirs, "Flying") {
		t.Fatal("the opponent's remembered creature did not gain Flying: RememberObjects$ Valid Creature.OppCtrl captured nobody")
	}
	if e.HasKeyword(ours, "Flying") {
		t.Fatal("the controller's own creature gained Flying: the capture was over-broad")
	}
}
