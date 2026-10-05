package compliance

import (
	"reflect"
	"sort"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

// foldedCollisions is the census of distinct corpus card faces whose names
// fold to the same key. FoldName drops case, diacritics and punctuation, so
// two cards can collide; FoldedNames is first-wins, and a printed or XMage
// name that folds to a colliding key could then resolve onto the other card.
//
// The list is a ratchet over the whole corpus (not just one manifest, which
// only guards cards a single set lists twice): a NEW collision fails loudly,
// and the fix is to pick the disambiguating exact-name branch for that
// carrier rather than let first-wins decide. "Waste Land" / "Wasteland" is
// the one carrier at FORGE_REF today; both are pure ASCII, so it predates the
// diacritic fold.
var foldedCollisions = []string{
	"Waste Land|Wasteland",
}

// TestNoNewFoldedCorpusCollision pins the corpus-wide fold collisions. It
// reads the corpus registry, so it skips without .cards; the printed-only
// census (TestPrintedManifestNamesFold) still runs and is the ratchet that
// catches new accented printed carriers.
func TestNoNewFoldedCorpusCollision(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	seen := map[string]string{}
	var got []string
	for _, c := range reg.Cards {
		for _, f := range c.Faces {
			k := FoldName(f.Name)
			if k == "" {
				continue
			}
			if prev, ok := seen[k]; ok && prev != f.Name {
				a, b := prev, f.Name
				if a > b {
					a, b = b, a
				}
				got = append(got, a+"|"+b)
			} else {
				seen[k] = f.Name
			}
		}
	}
	sort.Strings(got)
	got = dedupe(got)
	if !reflect.DeepEqual(got, foldedCollisions) {
		t.Errorf("corpus fold collisions changed:\n got  %v\n want %v", got, foldedCollisions)
	}
}

func dedupe(in []string) []string {
	if len(in) == 0 {
		return in
	}
	out := in[:1]
	for _, s := range in[1:] {
		if s != out[len(out)-1] {
			out = append(out, s)
		}
	}
	return out
}
