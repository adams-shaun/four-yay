package botpolicy_test

import (
	"testing"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// TestCmcOfFaceMatchesCmcOf: the face's load-time mana value (the board
// build's cmcOfFace) equals CmcOf over every corpus face and token.
func TestCmcOfFaceMatchesCmcOf(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	n := 0
	check := func(name, mc string, got int32, ok bool) {
		if !ok {
			t.Fatalf("%s: face mana value unbound", name)
		}
		if want := botpolicy.CmcOf(mc); got != want {
			t.Fatalf("%s: ManaCost %q: face mana value %d, CmcOf %d", name, mc, got, want)
		}
		n++
	}
	for _, c := range reg.AllCards() {
		for _, f := range c.Faces {
			if f != nil {
				v, ok := f.PrintedManaValue()
				check(f.Name, f.ManaCost, v, ok)
			}
		}
	}
	for _, c := range reg.Tokens {
		for _, f := range c.Faces {
			if f != nil {
				v, ok := f.PrintedManaValue()
				check(f.Name, f.ManaCost, v, ok)
			}
		}
	}
	if n == 0 {
		t.Fatal("no faces checked")
	}
}
