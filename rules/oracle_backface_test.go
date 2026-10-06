package rules

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

// backFaceCard is a transforming double-faced card whose back face is a
// creature with a distinct name, P/T and types (Vincent Valentine ->
// Galian Beast, 3/2 Legendary Creature Werewolf Beast). The test reads the
// registry's faces and asserts against them, so it cannot pass on a card that
// lost its second face.
const backFaceCard = "Vincent Valentine"

// TestOracleSetupBackFace pins the runner's "back_face" setup op: a battlefield
// card named there starts on its back face (face index 1), reported by its
// back-face name, P/T and types, and the scenario replays byte-identically.
//
// The precondition assertion (two faces, face 1 a creature whose name differs
// from face 0) is what makes the later assertions meaningful: without it a
// single-faced card would "pass" by never flipping.
func TestOracleSetupBackFace(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	c, ok := reg.Lookup(backFaceCard)
	if !ok || len(c.Faces) < 2 {
		t.Fatalf("%s is not a >=2-face card in the corpus (faces=%d)", backFaceCard, len(c.Faces))
	}
	front, back := c.Faces[0], c.Faces[1]
	if front.Name == back.Name {
		t.Fatalf("face 1 of %s has the same name as face 0 (%q); the test cannot tell the faces apart", backFaceCard, front.Name)
	}
	if !back.IsCreature() {
		t.Fatalf("face 1 of %s is not a creature; the P/T assertion would be vacuous", backFaceCard)
	}

	sc := `{"name":"backface","setup":{"p0":{"battlefield":["` + backFaceCard +
		`"],"back_face":["` + backFaceCard + `"]},"p1":{}},"steps":[]}`
	first, err := RunOracleScenarioJSON(reg, []byte(sc))
	if err != nil {
		t.Fatalf("first run: %v", err)
	}
	second, err := RunOracleScenarioJSON(reg, []byte(sc))
	if err != nil {
		t.Fatalf("second run: %v", err)
	}

	// The permanent must be present on face 1: its back-face name, P/T and
	// types, controlled by p0.
	var perm *OracleSnapPerm
	for i := range first.Snapshots {
		for j := range first.Snapshots[i].Permanents {
			p := &first.Snapshots[i].Permanents[j]
			if p.Controller == 0 && p.Name == back.Name {
				perm = p
			}
		}
	}
	if perm == nil {
		t.Fatalf("no p0 permanent named %q in any snapshot of the back-face scenario", back.Name)
	}
	if perm.PT != back.PT {
		t.Errorf("back-face permanent P/T = %q, want %q", perm.PT, back.PT)
	}
	if !oracleHasFold(perm.Types, "Creature") {
		t.Errorf("back-face permanent types = %v, want a Creature", perm.Types)
	}
	if perm.Name == front.Name {
		t.Errorf("permanent still reports the front-face name %q", front.Name)
	}

	// A scenario replays deterministically: two independent runs produce the
	// same snapshots and decisions.
	a, _ := json.Marshal(first)
	b, _ := json.Marshal(second)
	if !strings.EqualFold(string(a), string(b)) {
		t.Errorf("back-face scenario is not deterministic:\nfirst:  %s\nsecond: %s", a, b)
	}
}
