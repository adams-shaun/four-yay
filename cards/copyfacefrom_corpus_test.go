package cards_test

// Corpus-level regression guard for the byName priority change the
// CopyFaceFrom resolution forced (ticket agent-20260928T215303Z-ab37989c).
//
// A resolved CopyFaceFrom back face carries the REAL referenced spell's name.
// The referring card can sort before the referenced one (e/emeritus_of_conflict
// before l/lightning_bolt), so indexing back faces in plain sorted card order
// made Lookup("Lightning Bolt"), Lookup("Raise Dead"), Lookup("Reanimate") and
// ~20 others return a Prepare stub instead of the actual card -- breaking every
// decklist that names them. Registry.rebuildNameIndex now indexes front faces
// before back faces.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// TestNoBackFaceShadowsAFrontFace asserts the invariant directly over the real
// corpus: wherever a back face's name equals some card's front-face name,
// Lookup must return the front-name card. The >= 20 floor is the vacuity
// guard: the corpus has 23 such collisions (21 Prepare insets plus the two
// Split halves), so a parser/index change that removed them would fail here
// rather than pass silently.
func TestNoBackFaceShadowsAFrontFace(t *testing.T) {
	reg := testutil.CorpusRegistry(t)

	fronts := map[string]*cards.Card{}
	for _, c := range reg.Cards {
		if c == nil || len(c.Faces) == 0 || c.Faces[0].Name == "" {
			continue
		}
		k := cards.NormalizeName(c.Faces[0].Name)
		if _, ok := fronts[k]; !ok {
			fronts[k] = c
		}
	}

	checked := 0
	for _, c := range reg.Cards {
		if c == nil {
			continue
		}
		for i := range c.Faces {
			f := c.Faces[i]
			// Only faces whose identity may be non-native: back faces and
			// CopyFaceFrom-derived faces (a Split card's derived front is one).
			if f == nil || (i == 0 && f.CopyFaceFrom == "") {
				continue
			}
			k := cards.NormalizeName(f.Name)
			front, ok := fronts[k]
			if !ok {
				continue
			}
			got, ok := reg.Lookup(f.Name)
			if !ok || got != front {
				t.Errorf("face %q of %q (index %d) shadows the real card %q (Lookup returned %v)",
					f.Name, c.Faces[0].Name, i, front.Faces[0].Name, got)
			}
			checked++
		}
	}
	if checked < 20 {
		t.Fatalf("only %d non-native/front name collisions checked; want >= 20 (the test is vacuous otherwise)", checked)
	}
}
