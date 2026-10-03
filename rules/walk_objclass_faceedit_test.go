package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// TestWalkClassOfSlowToleratesInPlaceFaceEdit pins the verifier branch of
// walkClassOfSlow against the one legitimate way a class moves under an
// unchanged fingerprint: a test edits a face list in place with no event
// (faces are immutable once configured, so no event can express it). The
// sibling verifier walkClassTouch already tolerates this case via
// walkFacesEdited; walkClassOfSlow must too, or the whole rules test binary
// (walkSkipVerify) panics on the very state its sibling accepts.
func TestWalkClassOfSlowToleratesInPlaceFaceEdit(t *testing.T) {
	// A land with no static and no keyword: adding a Continuous static (and
	// thus the Goblin type it grants) moves mayPlayHot, staticOn and
	// staticOff, all face-derived.
	land := card(t, "Name:Probe Land\nTypes:Land\nA:AB$ Mana | Cost$ T | Produced$ G | SpellDescription$ x\nOracle:x\n")
	e := handEngine(t, land)
	e.walkClassesCatchUp()
	id := e.G.Zone(state.ZHand, 0)[0]
	o := e.G.Obj(id)
	if o == nil || o.Zone != state.ZHand {
		t.Fatalf("setup: object %d not in hand (got %+v)", id, o)
	}
	if !walkSkipVerify {
		t.Fatal("setup: walkSkipVerify is off, so the verifier branch under test never runs")
	}

	// Classify once, caching the class (c.set = true), so the next read
	// takes the verify branch rather than the fresh-compute branch.
	before := *e.walkClassOf(id)
	if !e.walkObjCls[uint(id)-1].set {
		t.Fatal("setup: the class was not cached")
	}

	// Edit the shared face in place, with no event: append a Continuous
	// static granting a creature type.
	f := o.Face()
	f.Statics = append(append([]cards.Static(nil), f.Statics...),
		cards.Static{Mode: "Continuous", Params: map[string]string{"AddType": "Goblin"}})

	// Precondition: this really is the in-place-face-edit case, and the
	// class bits really moved. A vacuous setup must fail here, not silently
	// pass.
	if !e.walkFacesEdited(o) {
		t.Fatal("precondition: walkFacesEdited is false, so the tolerance cannot apply")
	}
	want := e.computeWalkObjClass(o)
	if want.withoutFP() == before.withoutFP() {
		t.Fatalf("precondition: the class did not move under the face edit (before %+v)", before)
	}
	if !want.mayPlayHot || !want.staticOn {
		t.Fatalf("precondition: the edited class is not the expected hot one (%+v)", want)
	}

	// The verifier must refresh the cached class, not panic.
	got := e.walkClassOf(id)
	if got.withoutFP() != want.withoutFP() {
		t.Fatalf("class after in-place face edit = %+v, want rebuilt %+v", *got, want)
	}
	// The refresh must land in the cache, not a scratch copy.
	if cached := e.walkObjCls[uint(id)-1]; cached.withoutFP() != want.withoutFP() {
		t.Fatalf("cached class not refreshed: %+v, want %+v", cached, want)
	}
}
