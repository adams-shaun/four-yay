package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
)

// TestTriggerOfCopyFaceKeepsCopiedLine pins the fix for the copy-effect
// regression in the CR 712.8a face tag (triggerTaggedFace): an object under a
// copy effect shows its CopyFace as the active face, and its trigger line
// lives on that face -- never on the copier's printed Card.Faces. A pending
// trigger whose FaceTag names the copier's printed face index (FaceTag 1 for
// an untransformed copier) must therefore keep reading o.Face(), not
// o.Card.Faces[FaceTag-1].
//
// The regression it guards: checkFaceTriggers stamped FaceTag on EVERY matched
// printed T: line, and triggerOf then unconditionally read Card.Faces[FaceTag-1].
// For a Clone copying a creature with an OptionalDecider$ trigger, that read
// returned the copier's own (trigger-free) front face, so triggerOf said
// !ok and the copied "you may" trigger was silently treated as mandatory.
func TestTriggerOfCopyFaceKeepsCopiedLine(t *testing.T) {
	e := layerEngine(t)
	// The copier's own printed front face has NO trigger, so a fall-through to
	// Card.Faces[0] cannot accidentally find the copied line.
	id := onBoard(t, e, 0, "Name:Copier\nTypes:Creature\nPT:1/1\nOracle:x\n")
	o := e.G.Obj(id)
	if o == nil || o.Face() == nil {
		t.Fatal("copier is not on the battlefield")
	}
	if len(o.Card.Faces[0].Triggers) != 0 {
		t.Fatalf("fixture copier already has %d printed triggers; the test cannot tell the copied line apart", len(o.Card.Faces[0].Triggers))
	}

	// A copied creature whose only trigger is a "you may" ETB -- the exact
	// shape a Clone takes on.
	copied := card(t, "Name:Copied\nTypes:Creature\nPT:2/2\n"+
		"T:Mode$ ChangesZone | Origin$ Any | Destination$ Battlefield | TriggerZones$ Battlefield | OptionalDecider$ You | Execute$ TrigDraw | TriggerDescription$ When this enters, you may draw a card.\n"+
		"Oracle:x\n")
	if len(copied.Faces[0].Triggers) != 1 {
		t.Fatalf("copied face has %d triggers, want 1", len(copied.Faces[0].Triggers))
	}
	o.SetCopyFace(copied.Faces[0])
	if o.Face() != copied.Faces[0] {
		t.Fatalf("CopyFace was not applied: Face() = %p, want copied face %p", o.Face(), copied.Faces[0])
	}

	// checkFaceTriggers stamps FaceTag = fc.faceIdx+1; for a copy the active
	// face is recorded with the copier's printed FaceIdx (0 untransformed).
	pt := pendingTrigger{
		Source:  id,
		Idx:     0,
		SA:      copied.Faces[0].Triggers[0].Effect,
		FaceTag: o.FaceIdx + 1,
	}
	if pt.FaceTag != 1 {
		t.Fatalf("precondition: FaceTag = %d, want 1 (the copier's printed front face)", pt.FaceTag)
	}

	got, ok := e.triggerOf(pt)
	if !ok {
		t.Fatal("triggerOf returned !ok for the copied trigger; the printed-face branch stole the copier's own face")
	}
	if got.Effect != pt.SA {
		t.Errorf("triggerOf returned a different line than the copied one: got %p, want %p", got.Effect, pt.SA)
	}
	if spec := triggerOptionalSpec(got); spec != "You" {
		t.Errorf("copied OptionalDecider$ = %q, want %q; a copy's optional trigger must stay optional", spec, "You")
	}
	if _, optional, _ := e.optionalDecider(pt); !optional {
		t.Error("optionalDecider says the copied trigger is mandatory, want optional")
	}
}

// TestTriggerTaggedFaceRefusesCopy is the direct unit pin on the shared
// predicate: it is the ONE home for "does this pending trigger's line live on
// a printed face the source no longer shows", and an active CopyFace must
// always answer no.
func TestTriggerTaggedFaceRefusesCopy(t *testing.T) {
	e := layerEngine(t)
	// A two-faced copier, so FaceTag 2 is within o.Card.Faces and only the
	// CopyFace guard can refuse it.
	src := "Name:Front\nTypes:Creature\nPT:1/1\nAlternateMode:DoubleFaced\nOracle:x\n\nALTERNATE\n\nName:Back\nTypes:Creature\nPT:3/3\nOracle:x\n"
	id := onBoard(t, e, 0, src)
	o := e.G.Obj(id)
	sa := &cards.SA{}

	// A copy effect active: even a tag that disagrees with FaceIdx must not
	// take the Card.Faces branch. FaceTag 2 (the back face) against a copier
	// whose printed FaceIdx is 0 is the reachable disagreement.
	o.SetCopyFace(card(t, "Name:Copied\nTypes:Creature\nPT:2/2\nOracle:x\n").Faces[0])
	o.SetFaceIdx(0)
	pt := pendingTrigger{Source: id, Idx: 0, SA: sa, FaceTag: 2}
	if o.FaceIdx+1 == pt.FaceTag {
		t.Fatalf("precondition: FaceIdx+1 = %d equals FaceTag %d; the branch cannot be reached", o.FaceIdx+1, pt.FaceTag)
	}
	if triggerTaggedFace(o, pt) {
		t.Error("triggerTaggedFace took the printed-face branch while CopyFace is set")
	}
}

// TestTriggerTaggedFaceTakesPrintedFace is the positive half: without a copy,
// a tag that names a printed face the source no longer shows is exactly the
// CR 712.8a shape the branch exists for.
func TestTriggerTaggedFaceTakesPrintedFace(t *testing.T) {
	e := layerEngine(t)
	src := "Name:Front\nTypes:Creature\nPT:1/1\nAlternateMode:DoubleFaced\nOracle:x\n\nALTERNATE\n\nName:Back\nTypes:Creature\nPT:3/3\nOracle:x\n"
	id := onBoard(t, e, 0, src)
	o := e.G.Obj(id)
	if o.Card == nil || len(o.Card.Faces) != 2 {
		t.Fatalf("fixture is not a two-faced card: %d faces", len(o.Card.Faces))
	}
	// The source now shows its front face (FaceIdx 0) after the CR 712.8a
	// reset, so a tag naming the back face (FaceTag 2) must take the branch.
	o.SetFaceIdx(0)
	pt := pendingTrigger{Source: id, Idx: 0, SA: &cards.SA{}, FaceTag: 2}
	if !triggerTaggedFace(o, pt) {
		t.Error("triggerTaggedFace missed a printed back-face tag while the source shows its front face")
	}
	// The active face's own tag (FaceTag 1) stays bare.
	pt.FaceTag = 1
	if triggerTaggedFace(o, pt) {
		t.Error("triggerTaggedFace tagged the source's active face")
	}
}
