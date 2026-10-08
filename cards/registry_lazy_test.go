package cards

import (
	"sync"
	"testing"
)

// TestRegistryLazyConcurrent materializes one deck's worth of cards from 8
// goroutines at once and asserts the imaged registry hands out exactly one
// pointer per ordinal (spec §2.5 contract 2). Run it under -race.
func TestRegistryLazyConcurrent(t *testing.T) {
	corpusDir(t) // Skips when there is no corpus
	reg, err := OpenCorpus("../.cards")
	if err != nil {
		t.Fatal(err)
	}
	if reg.lazy == nil {
		t.Fatal("OpenCorpus did not return an imaged registry")
	}
	ords := make([]int, 0, 60)
	for i := 0; i < reg.Len() && len(ords) < 60; i += reg.Len() / 60 {
		ords = append(ords, i)
	}
	const workers = 8
	got := make([][]*Card, workers)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			out := make([]*Card, len(ords))
			for j := range ords {
				// Each worker walks the deck from a different offset so
				// first touches genuinely race.
				k := (j + w*7) % len(ords)
				out[k] = reg.Card(ords[k])
				if c, ok := reg.Lookup(out[k].Faces[0].Name); !ok || c == nil {
					t.Errorf("Lookup(%q) missed", out[k].Faces[0].Name)
				}
			}
			got[w] = out
		}(w)
	}
	wg.Wait()
	for w := 1; w < workers; w++ {
		for j := range ords {
			if got[w][j] != got[0][j] {
				t.Fatalf("ordinal %d: two pointers escaped", ords[j])
			}
		}
	}
	if n := reg.MaterializedCount(); n < len(ords) || n > len(ords)+len(ords) {
		t.Fatalf("MaterializedCount = %d for %d touched ordinals", n, len(ords))
	}
}
