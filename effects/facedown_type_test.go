package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

// TestFaceDownHidesPrintedSubtypes pins the CR 708.8 type gate every
// printed-face fallback shares (task d8b-permanents): a face-down battlefield
// permanent's type words are exactly its folded face-down set, never the
// hidden face's printed line. Before the gate, "Whenever a Detective you
// control enters" (Case of the Pilfered Proof) and "Whenever another Detective
// you control enters" (Perimeter Enforcer) matched a face-down Basilica
// Stalker entering disguised, because the fallback read the printed face.
func TestFaceDownHidesPrintedSubtypes(t *testing.T) {
	h := newHost(t, 2)
	stalker := mkCard(t, "Name:Basilica Stalker\nTypes:Creature Detective Vampire\nPT:2/3\nOracle:x\n")
	o := h.g.AddObject(stalker, 0)
	o.Zone = state.ZBattlefield
	o.FaceDown = true
	// Precondition: the printed face really carries the subtypes the gate
	// must hide, so a test that passed against a featureless face proves
	// nothing.
	for _, w := range []string{"Detective", "Vampire"} {
		if !stalker.Faces[0].TypeLineHas(w, 0) {
			t.Fatalf("precondition: printed face lacks %q", w)
		}
	}
	if hasType(o, "Detective") || hasType(o, "Vampire") {
		t.Fatal("face-down permanent matched a printed subtype (CR 708.8)")
	}
	if !hasType(o, "Creature") {
		t.Fatal("face-down permanent is not a Creature")
	}
	// The printed face-up object in the same zone still matches, so the gate
	// is the FaceDown flag's, not the zone's.
	o.FaceDown = false
	if !hasType(o, "Detective") {
		t.Fatal("face-up permanent lost its printed subtype")
	}
	// A FaceDownSetType$ replacement (Yedora's face-down Forest) replaces the
	// folded set: the replaced words match, Creature does not.
	o.FaceDown = true
	o.FaceDownSetType = "Land & Forest"
	if !hasType(o, "Forest") || !hasType(o, "Land") {
		t.Fatal("FaceDownSetType words do not match while face down")
	}
	if hasType(o, "Creature") {
		t.Fatal("Creature matched over a replaced face-down set")
	}
}
