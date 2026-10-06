package rules

import "testing"

// TestTriggerOfCopyFaceKeepsCopiedLine pins that the CR 712.8a printed-face
// lookup never steals a copy's trigger line: an object under a copy effect
// shows its CopyFace as the active face, and its trigger line lives on that
// face -- never on the copier's printed Card.Faces. For a Clone copying a
// creature with an OptionalDecider$ trigger, reading the copier's own
// (trigger-free) printed face would make triggerOf say !ok and silently treat
// the copied "you may" trigger as mandatory; the push amount must stay the
// plain live-face index.
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

	pt := pendingTrigger{
		Source: id,
		Idx:    0,
		SA:     copied.Faces[0].Triggers[0].Effect,
	}
	if got := triggerPushAmount(e.G, pt); got != 0 {
		t.Errorf("triggerPushAmount for the copied line = %d, want the plain live-face index 0", got)
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
