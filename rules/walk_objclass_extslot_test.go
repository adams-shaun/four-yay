package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// TestWalkClassToleratesFactsPublishedAfterCache pins the verifier branch
// against the second legitimate way a class moves under an unchanged
// fingerprint: a class cached while the face had no facts (classifyWalkFace's
// conservative nil branch -- manaHot, abAlways, abMask 0) when another engine
// publishes that face's ExtSlot. The corpus registry is one shared instance
// per process, so a *cards.Face is shared across parallel tests; an engine
// whose own compiledText does not contain a face reads nil until some other
// engine's table publishes it. The transition is nil -> one fixed set of
// facts, so the class can only become colder; the cache refresh is safe and
// must not panic. walkClassOfSlow and walkClassTouch are both exercised,
// each against its own cached object.
func TestWalkClassToleratesFactsPublishedAfterCache(t *testing.T) {
	// A fresh synthetic card no configuration has compiled: its face's
	// ExtSlot is nil until engine B (below) publishes it.
	c := card(t, "Name:ExtSlot Probe\nTypes:Artifact\nA:AB$ Draw | Cost$ 1 T | NumCards$ 1 | SpellDescription$ x\nOracle:x\n")
	face := c.Faces[0]

	// Engine A's config is mountains only, so it holds no facts for the
	// probe's face. Two objects share the card: one drives walkClassOf, the
	// other walkClassTouch.
	a := handEngine(t, c, c)
	a.walkClassesCatchUp()

	// Preconditions: the face has no published facts yet, and engine A
	// reads nil for it.
	if p := face.ExtSlot().Load(); p != nil {
		t.Fatal("precondition: the probe face had already published an ExtSlot")
	}
	if ff := a.walkFaceFactsOf(face); ff != nil {
		t.Fatalf("precondition: engine A reads facts for an uncompiled face (%+v)", ff)
	}

	hand := a.G.Zone(state.ZHand, 0)
	if len(hand) != 2 {
		t.Fatalf("setup: expected two probe objects in hand, got %v", hand)
	}
	idOf, idTouch := hand[0], hand[1]
	beforeOf := *a.walkClassOf(idOf)
	beforeTouch := *a.walkClassOf(idTouch)
	if !a.walkObjCls[uint(idOf)-1].set || !a.walkObjCls[uint(idTouch)-1].set {
		t.Fatal("setup: a class was not cached")
	}
	if !beforeOf.manaHot || !beforeOf.abAlways || beforeOf.abMask != 0 {
		t.Fatalf("precondition: cached class is not the conservative nil-facts class: %+v", beforeOf)
	}
	if beforeOf.withoutFP() != beforeTouch.withoutFP() {
		t.Fatalf("setup: the two probe objects differed before publication: %+v vs %+v", beforeOf, beforeTouch)
	}
	if !walkSkipVerify {
		t.Fatal("setup: walkSkipVerify is off, so the verifier branch under test never runs")
	}

	// Engine B's deck contains the probe card, so building its compiled text
	// publishes the face's facts (buildWalkFaceTable -> Face.ExtSlot).
	mountains := mountainDeck(t, 40)
	New(Config{Seed: 2, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{append([]*cards.Card{c}, mountains...), mountains}})

	// Preconditions after publication: the slot is live and the existing
	// in-place-edit tolerance does NOT apply (the facts are fullyCurrent, so
	// walkFacesEdited is false) -- this is exactly the case that panicked.
	if p := face.ExtSlot().Load(); p == nil {
		t.Fatal("precondition: engine B did not publish the probe face's facts")
	}
	if ff := a.walkFaceFactsOf(face); ff == nil {
		t.Fatal("precondition: engine A still reads nil after publication")
	}
	if a.walkFacesEdited(a.G.Obj(idOf)) || a.walkFacesEdited(a.G.Obj(idTouch)) {
		t.Fatal("precondition: walkFacesEdited is true, so the existing tolerance would mask the bug")
	}

	// The recomputed class is colder than the cached conservative one and
	// the move is in the tolerated direction.
	wantOf := a.computeWalkObjClass(a.G.Obj(idOf))
	if wantOf.withoutFP() == beforeOf.withoutFP() {
		t.Fatal("precondition: the class did not move after publication")
	}
	if !walkClassAtLeastAsHot(beforeOf, wantOf) {
		t.Fatalf("precondition: the move is not the tolerated (colder) direction: %+v -> %+v", beforeOf, wantOf)
	}

	// With the fix neither call panics, and each refreshes to the recomputed
	// class.
	gotOf := *a.walkClassOf(idOf)
	if gotOf.withoutFP() != wantOf.withoutFP() {
		t.Fatalf("walkClassOf returned %+v, want recomputed %+v", gotOf, wantOf)
	}
	a.walkClassTouch(a.G.Obj(idTouch))
	wantTouch := a.computeWalkObjClass(a.G.Obj(idTouch))
	gotTouch := a.walkObjCls[uint(idTouch)-1]
	if gotTouch.withoutFP() != wantTouch.withoutFP() {
		t.Fatalf("walkClassTouch refreshed to %+v, want recomputed %+v", gotTouch, wantTouch)
	}
}
