package cards

import (
	"strings"
	"testing"
)

// TestWordSetsMatchStringScans pins the interned type-word and keyword-head
// bitsets to the EqualFold scans they replace, over every corpus face and
// every word the corpus prints (plus case variants and absent words).
func TestWordSetsMatchStringScans(t *testing.T) {
	reg := compiledCorpus(t)
	var faces []*Face
	typeQ := map[string]bool{"creature": true, "CREATURE": true, "NotAType": true, "": true}
	kwQ := map[string]bool{"flying": true, "FLASH": true, "NotAKeyword": true, "": true}
	for _, c := range reg.Cards {
		for _, f := range c.Faces {
			if f == nil {
				continue
			}
			faces = append(faces, f)
			for _, w := range f.Types {
				typeQ[w] = true
			}
			for _, k := range f.Keywords {
				kwQ[KeywordHead(k)] = true
			}
		}
	}
	unbound := 0
	for _, f := range faces {
		if !f.typeWordsValid() || !f.kwHeadsValid() {
			unbound++
		}
	}
	if unbound > len(faces)/100 {
		t.Fatalf("%d of %d faces have no bound word sets", unbound, len(faces))
	}
	for w := range typeQ {
		id := InternTypeWord(w)
		for _, f := range faces {
			want := false
			for _, x := range f.Types {
				if strings.EqualFold(x, w) {
					want = true
				}
			}
			if got := f.TypeLineHas(w, id); got != want {
				t.Fatalf("%s TypeLineHas(%q)=%v, scan %v", f.Name, w, got, want)
			}
		}
	}
	for k := range kwQ {
		id := InternKeywordHead(k)
		for _, f := range faces {
			want := false
			for _, x := range f.Keywords {
				if strings.EqualFold(KeywordHead(x), k) {
					want = true
				}
			}
			if got := f.KeywordLinesHaveHead(k, id); got != want {
				t.Fatalf("%s KeywordLinesHaveHead(%q)=%v, scan %v", f.Name, k, got, want)
			}
		}
	}
}

// TestWordSetsGuardReplacedSlices: a face struct copy whose Types slice was
// replaced must not answer from the original's bitset.
func TestWordSetsGuardReplacedSlices(t *testing.T) {
	f := &Face{Types: []string{"Creature", "Elf"}, Keywords: []string{"Flying"}}
	f.deriveWordSets()
	elf, goblin, flying := InternTypeWord("Elf"), InternTypeWord("Goblin"), InternKeywordHead("Flying")
	if !f.TypeLineHas("Elf", elf) || f.TypeLineHas("Goblin", goblin) || !f.KeywordLinesHaveHead("flying", flying) {
		t.Fatal("bound face answers wrong")
	}
	cp := *f
	cp.Types = []string{"Creature", "Goblin"}
	cp.Keywords = nil
	if cp.TypeLineHas("Elf", elf) || !cp.TypeLineHas("Goblin", goblin) || cp.KeywordLinesHaveHead("Flying", flying) {
		t.Fatal("copied face with replaced slices answered from the stale bitset")
	}
}

// TestWordIDOfConcurrent exercises the front caches and interners from many
// goroutines (run under -race).
func TestWordIDOfConcurrent(t *testing.T) {
	words := []string{"Creature", "Elf", "Goblin", "Land", "Forest", "Wizard", "Human", "Artifact"}
	want := make([]TypeWordID, len(words))
	for i, w := range words {
		want[i] = InternTypeWord(w)
	}
	done := make(chan bool)
	for g := 0; g < 8; g++ {
		go func() {
			ok := true
			for n := 0; n < 2000; n++ {
				i := n % len(words)
				if TypeWordIDOf(words[i]) != want[i] || KeywordHeadIDOf(words[i]) != InternKeywordHead(words[i]) {
					ok = false
				}
			}
			done <- ok
		}()
	}
	for g := 0; g < 8; g++ {
		if !<-done {
			t.Fatal("concurrent lookup disagreed with the interner")
		}
	}
}

// TestTypeMaskPredicatesMatchSwitch pins each fixed-word predicate's mask to
// typeMaskFor of its word (hasTypeMask's contract).
func TestTypeMaskPredicatesMatchSwitch(t *testing.T) {
	for w, m := range map[string]TypeMask{"Land": TypeLand, "Basic": TypeBasic, "Legendary": TypeLegendary,
		"World": TypeWorld, "Creature": TypeCreature, "Instant": TypeInstant, "Sorcery": TypeSorcery,
		"Artifact": TypeArtifact, "Spacecraft": TypeSpacecraft, "Vehicle": TypeVehicle,
		"Enchantment": TypeEnchantment, "Planeswalker": TypePlaneswalker, "Battle": TypeBattle, "Room": TypeRoom} {
		if got := typeMaskFor(w); got != m {
			t.Fatalf("typeMaskFor(%q) = %v, predicate uses %v", w, got, m)
		}
	}
}

// TestCompiledTypeMaskMatchesCatalogRow: every bound face's copied mask is
// its catalog row's.
func TestCompiledTypeMaskMatchesCatalogRow(t *testing.T) {
	reg := compiledCorpus(t)
	n := 0
	for _, c := range reg.Cards {
		for _, f := range c.Faces {
			if f == nil || f.compiledCatalog == nil || f.compiledID == 0 {
				continue
			}
			if int(f.compiledID) > len(f.compiledCatalog.Faces) {
				t.Fatalf("%s: compiledID out of range", f.Name)
			}
			if got, want := f.compiledTypeMask, f.compiledCatalog.Faces[f.compiledID-1].TypeMask; got != want {
				t.Fatalf("%s: copied TypeMask %v, row %v", f.Name, got, want)
			}
			n++
		}
	}
	if n == 0 {
		t.Skip("corpus registry has no compiled catalog bound")
	}
}
