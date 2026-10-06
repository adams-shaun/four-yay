package templates

import (
	"testing"

	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// requirement finds the level-B requirement with the given key.
func requirement(t *testing.T, card, key string) levelb.Requirement {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	c, ok := reg.Lookup(card)
	if !ok {
		t.Fatalf("%s not in corpus", card)
	}
	for _, r := range levelb.Requirements(c) {
		if r.Key == key {
			return r
		}
	}
	t.Fatalf("%s has no requirement %q", card, key)
	return levelb.Requirement{}
}

// TestFaceOneTemplatesServeBackFaceSetup pins that a face-0 template reaches a
// requirement on face 1 of a DoubleFaced/Modal card and emits the back-face
// setup op for it, instead of the old "face 1" gap. It names one card per
// family: Dowsing Device's back-face activate, Vincent Valentine's back-face
// trigger, and Eirdu, Carrier of Dawn's back-face attack.
func TestFaceOneTemplatesServeBackFaceSetup(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, tc := range []struct{ card, key string }{
		{"Dowsing Device", "activate#1.0"},
		{"Vincent Valentine", "trigger#1.0"},
		{"Eirdu, Carrier of Dawn", "combat#1.attack"},
	} {
		t.Run(tc.card+"/"+tc.key, func(t *testing.T) {
			req := requirement(t, tc.card, tc.key)
			if req.Face != 1 {
				t.Fatalf("requirement %s has face %d, want 1", tc.key, req.Face)
			}
			// The gap is what the ticket removes: a servable face must carry
			// neither a gap nor a ".face:N" sub-family.
			if req.Gap != "" {
				t.Fatalf("requirement %s still carries gap %q", tc.key, req.Gap)
			}
			item, skip := GenerateB(reg, tc.card, req)
			if skip != nil {
				t.Fatalf("GenerateB(%s, %s) skipped: %s", tc.card, tc.key, skip.Reason)
			}
			p0, ok := item.Scenario.Setup["p0"]
			if !ok {
				t.Fatalf("scenario has no p0 setup")
			}
			// Precondition: the card is on p0's battlefield (a back face only
			// means anything for a card that is actually there).
			if !containsString(p0.Battlefield, tc.card) {
				t.Fatalf("p0 battlefield %v does not contain %s", p0.Battlefield, tc.card)
			}
			if !containsString(p0.BackFace, tc.card) {
				t.Fatalf("p0 back_face %v does not contain %s", p0.BackFace, tc.card)
			}
		})
	}
}
