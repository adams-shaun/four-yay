package searchprobe

import (
	"math"
	"testing"
)

// unseenMountainDeck is two known Mountain copies (IDs 1,2) plus two copies the
// attempt's observer has not introduced (IDs 3,4). Only the !Known copies may
// fill an Unseen position.
func unseenMountainDeck() []proposalCard {
	return []proposalCard{
		{ID: 1, Name: "Mountain", Known: true},
		{ID: 2, Name: "Mountain", Known: true},
		{ID: 3, Name: "Mountain", Known: false},
		{ID: 4, Name: "Mountain", Known: false},
	}
}

func TestUnseenPositionCountsOnlyUnknownCopies(t *testing.T) {
	cards := unseenMountainDeck()
	// Precondition: the deck actually mixes known and unknown copies, so the
	// two classes under comparison differ. A deck of all-known or all-unknown
	// cards would make this test vacuous.
	known, unknown := 0, 0
	for _, card := range cards {
		if card.Known {
			known++
		} else {
			unknown++
		}
	}
	if known == 0 || unknown == 0 {
		t.Fatalf("deck precondition failed: known=%d unknown=%d", known, unknown)
	}

	positions := []positionConstraint{{Index: 0, Name: "Mountain", Unseen: true}}
	plan, err := newConstrainedPermutation(cards, positions, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	// 4! total physical orders, of which only the 2 unseen copies may sit at
	// position 0: 2 * 3! = 12 completions, weight 12/24 = 1/2.
	want := int64(12)
	if got := plan.total.Int64(); got != want {
		t.Fatalf("total completions = %d, want %d (known copies leaked into an unseen position)", got, want)
	}
	if got := math.Exp(plan.weight); math.Abs(got-0.5) > 1e-12 {
		t.Fatalf("weight = %g, want 0.5", got)
	}

	// Enumerate every rank in the support and prove the unranking is a
	// bijection onto exactly the 12 unseen-first physical orders.
	seen := make(map[string]bool)
	for rank := 0; rank < int(want); rank++ {
		order, logWeight, compatible, err := plan.sample(&rankRandom{value: uint64(rank)})
		if err != nil || !compatible {
			t.Fatalf("rank %d: compatible=%v err=%v", rank, compatible, err)
		}
		if math.Abs(math.Exp(logWeight)-0.5) > 1e-12 {
			t.Fatalf("rank %d: weight=%g want 0.5", rank, math.Exp(logWeight))
		}
		if order[0] != 3 && order[0] != 4 {
			t.Fatalf("rank %d: unseen position drew known copy %d (order %v)", rank, order[0], order)
		}
		key := keyIDs(order)
		if seen[key] {
			t.Fatalf("rank %d: duplicate order %v", rank, order)
		}
		seen[key] = true
	}
	if len(seen) != int(want) {
		t.Fatalf("sampled %d unique orders, want %d", len(seen), want)
	}

	// A free position may take either class, but the unseen position after it
	// must still be satisfied from the remaining unknown copies.
	freeFirst := []positionConstraint{{Index: 0, Name: "Mountain"}, {Index: 1, Name: "Mountain", Unseen: true}}
	plan2, err := newConstrainedPermutation(cards, freeFirst, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := plan2.total.Int64(); got != want {
		t.Fatalf("free-then-unseen total = %d, want %d", got, want)
	}
	seen2 := make(map[string]bool)
	for rank := 0; rank < int(want); rank++ {
		order, _, compatible, err := plan2.sample(&rankRandom{value: uint64(rank)})
		if err != nil || !compatible {
			t.Fatalf("rank %d: compatible=%v err=%v", rank, compatible, err)
		}
		if order[1] != 3 && order[1] != 4 {
			t.Fatalf("rank %d: second unseen position drew known copy %d (order %v)", rank, order[1], order)
		}
		key := keyIDs(order)
		if seen2[key] {
			t.Fatalf("rank %d: duplicate order %v", rank, order)
		}
		seen2[key] = true
	}
	if len(seen2) != int(want) {
		t.Fatalf("free-then-unseen sampled %d unique orders, want %d", len(seen2), want)
	}
}

func TestUnseenPositionWithoutUnknownCopiesIsIncompatible(t *testing.T) {
	cards := []proposalCard{
		{ID: 1, Name: "Mountain", Known: true},
		{ID: 2, Name: "Mountain", Known: true},
	}
	unknown := 0
	for _, card := range cards {
		if !card.Known {
			unknown++
		}
	}
	if unknown != 0 {
		t.Fatalf("deck precondition failed: unknown=%d", unknown)
	}
	positions := []positionConstraint{{Index: 0, Name: "Mountain", Unseen: true}}
	plan, err := newConstrainedPermutation(cards, positions, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if plan.total.Sign() != 0 {
		t.Fatalf("total = %s, want 0 when no unknown copy can fill an unseen position", plan.total)
	}
	_, _, compatible, err := sampleConstrainedPermutation(cards, positions, nil, &rankRandom{})
	if err != nil {
		t.Fatal(err)
	}
	if compatible {
		t.Fatal("plan with no unknown copy was reported compatible")
	}
}

func TestUnseenPositionWithFixedObjectIsRejected(t *testing.T) {
	cards := unseenMountainDeck()
	positions := []positionConstraint{{Index: 0, Name: "Mountain", Obj: 3, Unseen: true}}
	if _, err := newConstrainedPermutation(cards, positions, nil, nil); err == nil {
		t.Fatal("position pinned to an exact object and marked unseen was accepted")
	}
}
