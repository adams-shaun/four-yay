package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestBarrinsSpiteSameControllerCapacity pins the real mandatory
// TargetsWithSameController$ shape at the resolution ask. Two legal creatures
// split across controllers cannot satisfy a two-target same-controller ask by
// being offered as an ordinary target decision; the ask must fizzle instead
// of handing a bot an answer that the submit gate rejects forever.
func TestBarrinsSpiteSameControllerCapacity(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	card, ok := reg.Lookup("Barrin's Spite")
	if !ok {
		t.Fatal("Barrin's Spite missing from corpus")
	}
	sa := card.Faces[0].SpellAbility()
	if sa == nil || sa.Params["TargetsWithSameController"] != "True" ||
		sa.Params["TargetMin"] != "2" || sa.Params["TargetMax"] != "2" {
		t.Fatalf("fixture precondition: SA = %+v, want mandatory same-controller pair", sa)
	}
	e := newSeats(t, 2)
	a, b := bearPermanent(t, e, 0), bearPermanent(t, e, 1)
	for _, id := range []state.ObjID{a, b} {
		o := e.G.Obj(id)
		if o == nil || o.Zone != state.ZBattlefield || (o.Controller != 0 && o.Controller != 1) {
			t.Fatalf("fixture precondition: bear %d is not a battlefield creature under seat 0 or 1", id)
		}
	}
	if e.G.Obj(a).Controller == e.G.Obj(b).Controller {
		t.Fatal("fixture precondition: legal bears must be split across controllers")
	}
	// The SA itself is the real card's target declaration; both candidates are
	// legal, and the only shortage is the set constraint.
	e.askTarget(0, 0, sa)
	if d := e.Pending(); d != nil && d.Kind == decision.KTarget {
		t.Fatalf("unsatisfiable same-controller target decision was posed: %+v", d)
	}
	if !hasEventText(e, "countered: no legal targets") {
		t.Fatal("mandatory same-controller ask did not fizzle")
	}
}
