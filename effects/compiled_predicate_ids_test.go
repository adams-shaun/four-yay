package effects

import (
	"sync"
	"testing"
)

// TestPredicateProgramMembershipByID: the id-bitset membership answers the
// text search's membership, first call and repeat, from many goroutines.
func TestPredicateProgramMembershipByID(t *testing.T) {
	members := []string{"Creature.YouCtrl", "Land.nonBasic", "Card.Self", "Permanent.nonLand+YouCtrl"}
	others := []string{"Creature.OppCtrl", "Artifact", "Enchantment.YouOwn"}
	ps := CompilePredicatePrograms(members)
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := 0; n < 200; n++ {
				for _, spec := range members {
					if _, ok := ps.programFor(compiledSpecFor(spec), spec); !ok {
						t.Errorf("%q: member not found", spec)
					}
				}
				for _, spec := range others {
					if _, ok := ps.programFor(compiledSpecFor(spec), spec); ok {
						t.Errorf("%q: non-member found", spec)
					}
				}
			}
		}()
	}
	wg.Wait()
	// An unretained spec (id 0) falls back to the text search.
	if _, ok := ps.programFor(&compiledSpec{}, "Land.nonBasic"); !ok {
		t.Fatal("id-0 member not found by text")
	}
	if ps.Len() != len(members) {
		t.Fatalf("Len = %d", ps.Len())
	}
}
