package rules

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules/chars"
)

// TestNameUniverseImagedMatchesSlice: the imaged registry's pointer-free
// universe answers every whole-universe name query exactly as the plain
// slice of the same cards does (the pre-S3 representation), over the real
// corpus.
func TestNameUniverseImagedMatchesSlice(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	img := reg.Universe()
	if img != reg.Universe() {
		t.Fatal("Registry.Universe is not one pointer")
	}
	sl := cards.UniverseOf(reg.AllCards())
	if img.Len() != sl.Len() {
		t.Fatalf("Len %d, slice %d", img.Len(), sl.Len())
	}
	for i := 0; i < sl.Len(); i++ {
		if img.Name(i) != sl.Name(i) {
			t.Fatalf("Name(%d) = %q, slice %q", i, img.Name(i), sl.Name(i))
		}
	}
	if !slices.Equal(img.Names(), sl.Names()) {
		t.Fatal("Names differ")
	}
	if a, b := chars.CorpusLandTypeWords(img), chars.CorpusLandTypeWords(sl); !slices.Equal(a, b) || len(a) == 0 {
		t.Fatalf("LandTypeWords differ: %v vs %v", a, b)
	}
	for j, n := range sl.Names() {
		if j%37 != 0 {
			continue
		}
		ai, aok := img.FirstByName(n)
		bi, bok := sl.FirstByName(n)
		if ai != bi || aok != bok {
			t.Fatalf("FirstByName(%q) = %d,%v slice %d,%v", n, ai, aok, bi, bok)
		}
		k := cards.NormalizeName(n)
		ai, aok = img.FirstByNormalized(k)
		bi, bok = sl.FirstByNormalized(k)
		if ai != bi || aok != bok {
			t.Fatalf("FirstByNormalized(%q) = %d,%v slice %d,%v", k, ai, aok, bi, bok)
		}
	}
}
