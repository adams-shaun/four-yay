package oraclegen

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
)

// A search-and-shuffle only removes a card from a library whose filler is
// uniform, so its order is deterministic in both engines and stays compared.
func TestShuffleMarkSkipsSearchAndShuffle(t *testing.T) {
	reg, err := cards.SharedCorpus(filepath.Join("..", "..", ".cards"))
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]bool{
		"Circuitous Route":        false,
		"Expedition Map":          false,
		"Fblthp, Impossibly Lost": true,
	} {
		c, ok := reg.Lookup(name)
		if !ok {
			t.Fatalf("%s absent from corpus", name)
		}
		f := c.Faces[0]
		if !strings.Contains(strings.ToLower(f.Oracle), "shuffle") && want == false {
			t.Fatalf("precondition: %s no longer mentions a shuffle", name)
		}
		if got := CanShuffleLibrary(f); got != want {
			t.Errorf("CanShuffleLibrary(%s) = %v, want %v", name, got, want)
		}
	}
}
