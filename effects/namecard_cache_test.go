package effects

import (
	"reflect"
	"slices"
	"sync"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestNameChoicesCacheMatchesUncachedOnCorpus pins the memo's soundness on
// the REAL corpus: for every spec pureNameSpec admits (the corpus's NameCard
// ValidCards$ forms plus the description fallbacks), the memoised list equals
// the uncached filter walk, a second call returns the same shared list, and
// the memoised options are exactly the per-ask shape.
func TestNameChoicesCacheMatchesUncachedOnCorpus(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	g := state.NewGame(names(2))
	g.NameUniverse = reg.Universe()
	g.NameUniverseNames = NameUniverseNames(reg.Universe())
	if again := NameUniverseNames(reg.Universe()); &again[0] != &g.NameUniverseNames[0] {
		t.Fatal("NameUniverseNames recomputed for the same universe")
	}
	if !slices.Equal(g.NameUniverseNames, cards.UniverseOf(reg.AllCards()).Names()) {
		t.Fatal("memoised universe names differ from a fresh build")
	}
	specs := []string{"", "Card.nonLand", "Card.nonBasic", "Creature", "Card.Creature",
		"Land", "Card.nonLand+nonCreature", "Card.nonLand+nonArtifact",
		"Card.Land+nonBasic", "Card.Artifact"}
	for _, spec := range specs {
		if !pureNameSpec(spec) {
			t.Fatalf("pureNameSpec(%q) = false", spec)
		}
		for _, list := range []string{"", "Island,Forest,Black Lotus"} {
			want := nameChoicesFiltered(g, spec, list, false, nil)
			got := NameChoicesFromList(g, spec, "", list)
			if !slices.Equal(got, want) {
				t.Fatalf("spec %q list %q: cached %d names, uncached %d", spec, list, len(got), len(want))
			}
			again := NameChoicesFromList(g, spec, "", list)
			if len(got) > 0 && &again[0] != &got[0] {
				t.Fatalf("spec %q list %q: second call did not hit the memo", spec, list)
			}
			opts := NameOptions(got, 1)
			if len(opts) != len(got) {
				t.Fatalf("spec %q: %d options for %d names", spec, len(opts), len(got))
			}
			for i, o := range opts {
				if !reflect.DeepEqual(o, decision.Option{Index: i, Kind: "name", Label: got[i], Player: 1}) {
					t.Fatalf("spec %q option %d = %+v", spec, i, o)
				}
			}
		}
	}
	for _, impure := range []string{"Creature.cmcEQX", "Card.ManaCost=Imprinted", "Creature.ManaCost=Equipped", "Card.nonLand,Creature", "Card.!Land"} {
		if pureNameSpec(impure) {
			t.Fatalf("pureNameSpec(%q) = true, want false", impure)
		}
	}
}

// TestNameChoicesCacheConcurrent exercises the process-wide memo from
// parallel games (botbench's shape); run under -race.
func TestNameChoicesCacheConcurrent(t *testing.T) {
	universe := cards.UniverseOf(namecardUniverse(t))
	var wg sync.WaitGroup
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				g := state.NewGame(names(2))
				g.NameUniverse = universe
				g.NameUniverseNames = NameUniverseNames(universe)
				got := NameChoices(g, "Card.nonLand", "")
				if len(got) != 1 || got[0] != "Bear" {
					t.Errorf("NameChoices = %v", got)
					return
				}
				if opts := NameOptions(got, state.PlayerID(i%2)); len(opts) != 1 || opts[0].Player != state.PlayerID(i%2) {
					t.Errorf("NameOptions = %+v", opts)
					return
				}
			}
		}()
	}
	wg.Wait()
}
