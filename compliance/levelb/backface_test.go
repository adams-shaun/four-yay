package levelb_test

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
)

// twoFacedActivate builds a two-face card whose face 1 carries one activated
// ability, so Requirements emits an activate#1.0 requirement under any layout.
func twoFacedActivate(mode string) *cards.Card {
	return &cards.Card{
		AlternateMode: mode,
		Faces: []*cards.Face{
			{Name: "Front", Types: []string{"Creature"}},
			{Name: "Back", Types: []string{"Creature"}, Abilities: []*cards.SA{{Kind: "AB", API: "Pump"}}},
		},
	}
}

// faceRequirement returns the requirement with key from c, failing the test if
// the requirement does not exist.
func faceRequirement(t *testing.T, c *cards.Card, key string) levelb.Requirement {
	t.Helper()
	for _, r := range levelb.Requirements(c) {
		if r.Key == key {
			return r
		}
	}
	t.Fatalf("%s has no requirement %q", key, key)
	return levelb.Requirement{}
}

// TestFaceOneGapIsLiftedForBackFaceLayouts pins the classifier change: a
// requirement on a face after 0 of a DoubleFaced or Modal card carries no
// "face N" gap (its own sub-family is kept, so a face-0 template can serve it
// with a back-face setup), while the same shape under any other layout still
// gets the face gap.
func TestFaceOneGapIsLiftedForBackFaceLayouts(t *testing.T) {
	served := []string{"DoubleFaced", "Modal"}
	for _, mode := range served {
		r := faceRequirement(t, twoFacedActivate(mode), "activate#1.0")
		if r.Gap != "" {
			t.Errorf("%s activate#1.0 gap = %q, want none", mode, r.Gap)
		}
		if strings.Contains(r.Sub, ".face:") {
			t.Errorf("%s activate#1.0 sub = %q, want no face suffix", mode, r.Sub)
		}
		if r.CoveredByA {
			t.Errorf("%s activate#1.0 is covered by level A; a back face is not", mode)
		}
		if r.Sub != "activate.battlefield" {
			t.Errorf("%s activate#1.0 sub = %q, want activate.battlefield", mode, r.Sub)
		}
	}

	// Any other layout with a second face keeps the gap: its face 1 is not a
	// permanent a back-face setup can place.
	for _, mode := range []string{"Adventure", "Split", "Flip", "Prepare", "Meld", "Specialize", "Omen"} {
		r := faceRequirement(t, twoFacedActivate(mode), "activate#1.0")
		if r.Gap != "face 1" {
			t.Errorf("%s activate#1.0 gap = %q, want %q", mode, r.Gap, "face 1")
		}
		if !strings.HasSuffix(r.Sub, ".face:1") {
			t.Errorf("%s activate#1.0 sub = %q, want a .face:1 suffix", mode, r.Sub)
		}
	}

	// A face-0 requirement is untouched under every layout: the lift must not
	// change the SHAPE of a card's first face.
	front := twoFacedActivate("Adventure")
	front.Faces[0].Abilities = []*cards.SA{{Kind: "AB", API: "Pump"}}
	r := faceRequirement(t, front, "activate#0.0")
	if r.Gap != "" || r.Sub != "activate.battlefield" {
		t.Errorf("activate#0.0 = gap %q sub %q, want empty gap and activate.battlefield", r.Gap, r.Sub)
	}
}
