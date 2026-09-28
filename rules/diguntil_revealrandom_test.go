package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

// TestDigUntilRevealRandomOrderShuffledAndDeterministic pins the two halves of
// RevealRandomOrder$ True on a REAL corpus carrier (Heirloom Blade, DB$
// DigUntil, RevealedDestination$ Library, RevealedLibraryPosition$ -1):
//
//   - the public reveal Note still names the revealed cards in SCAN order
//     (reveal order is a reveal-time fact and must NOT be shuffled), and
//   - the returned bottom pile is a different permutation of the revealed
//     cards — the seeded-random return order — replayed byte-identically.
//
// The no-match fixture (no Bear in the library) is used because it is the only
// one whose returned pile has more than one card, so "the reveal order and the
// return order differ" is observable. Heirloom Blade is in NO repo deck and NO
// legacy golden deck, so no chain head depends on it.
func TestDigUntilRevealRandomOrderShuffledAndDeterministic(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg, _, _, _, giantID := heirloomTestEngine(t, reg, false)
	d := heirloomMurderBearer(t, e, findHandCard(t, e, "Murder"), mustBearer(t, e))
	before := append([]state.ObjID(nil), e.G.Zone(state.ZLibrary, 0)...)
	// Precondition: the library is the KNOWN exact order the helper dealt, and
	// it is longer than one card (otherwise no order could shuffle and this
	// test would pass vacuously).
	if before[0] != giantID {
		t.Fatalf("library head = %d, want the Hill Giant %d (fixture order broken)", before[0], giantID)
	}
	if len(before) < 3 {
		t.Fatalf("library length = %d, want >= 3 for an observable shuffle", len(before))
	}
	submitChoices(t, e, d.Options[0].Index) // "yes"
	passUntilStackEmpty(t, e, 20)

	// The public reveal Note is the pre-resolution order, card for card.
	if n := countPublicRevealNote(e, before); n != 1 {
		t.Fatalf("public reveal Notes naming the whole library in scan order = %d, want 1", n)
	}
	after := e.G.Zone(state.ZLibrary, 0)
	if len(after) != len(before) {
		t.Fatalf("library length %d -> %d, want unchanged", len(before), len(after))
	}
	// The returned pile is a permutation of the pre-resolution library...
	seen := map[state.ObjID]bool{}
	for _, id := range before {
		seen[id] = true
	}
	differ := false
	for i := range before {
		if !seen[after[i]] {
			t.Fatalf("library[%d] = %d is not a permutation of the pre-resolution library %v", i, after[i], before)
		}
		if after[i] != before[i] {
			differ = true
		}
	}
	// ...and it is NOT the scan order the reveal Note named (the mutation
	// this test exists to catch: the old existing-order stand-in reproduced
	// `before` exactly).
	if !differ {
		t.Fatalf("returned library %v equals the scan order; RevealRandomOrder$ must shuffle the bottom return", after)
	}
	replayCheck(t, e, cfg)
}
