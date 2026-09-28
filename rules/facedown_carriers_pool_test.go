// facedown_carriers_pool_test.go — the other half of the face-down carrier
// discipline TestFaceDownCarriersAreNotInRepoDecks asserts. That test scans
// only LegacyDeckNames, the closed 12-deck pool rules/heads_test.go's TestHeads
// seats from. This one pins, from the deck-import side, that the carriers the
// Deadly Disguise precon legitimately brings in stay OUT of that pool, so the
// narrowing in the guard is a measured decision and not an accidental blind
// spot. It is the class-level check: any future deck that carries a carrier
// must be kept out of the head-pinned pool, and the pool itself must stay the
// closed 12.
package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

// TestFaceDownCarriersAreOutsideTheHeadPinnedPool asserts the Deadly Disguise
// precon carries at least one bare-FaceDown$ ChangeZone carrier (precondition:
// without this the assertions below are vacuous) and that neither the deck nor
// any of its carriers has been swept into the head-pinned pool.
func TestFaceDownCarriersAreOutsideTheHeadPinnedPool(t *testing.T) {
	t.Parallel()
	// PRECONDITION: the imported deck really does carry at least one of the
	// carriers the face-down head pins act on. If it stopped, this test would
	// be proving nothing about the deck and must fail instead of pass.
	deck := testutil.RepoDeckFile(t, "deadly-disguise")
	inDeck := 0
	for _, c := range deck.Cards {
		if faceDownChangeZoneCarriers[c.Name] {
			inDeck++
		}
	}
	if inDeck == 0 {
		t.Fatal("deadly-disguise carries no face-down ChangeZone carrier; the pool-disjointness check below would be vacuous")
	}

	// PRECONDITION: the head-pinned pool is non-empty, so the loop is real.
	pool := testutil.LegacyDeckNames()
	if len(pool) == 0 {
		t.Fatal("LegacyDeckNames() is empty; the disjointness check would be vacuous")
	}

	for _, name := range pool {
		if name == "deadly-disguise" {
			t.Fatalf("deadly-disguise is in the head-pinned pool %v while carrying %d face-down carriers; TestHeads is no longer safe", pool, inDeck)
		}
		f, err := testutil.LoadRepoDeckFile(name)
		if err != nil {
			t.Fatalf("load head-pinned deck %s: %v", name, err)
		}
		for _, c := range f.Cards {
			if faceDownChangeZoneCarriers[c.Name] {
				t.Errorf("head-pinned deck %s carries face-down ChangeZone carrier %q; TestHeads is no longer safe", name, c.Name)
			}
		}
	}
}
