package searchprobe

import (
	"fmt"
	"math"
	"math/big"
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/state"
)

type rankRandom struct{ value uint64 }

func (r *rankRandom) Uint64() uint64 { return r.value }

func TestConstrainedPermutationExactDistribution(t *testing.T) {
	cards := []proposalCard{{ID: 1, Name: "A"}, {ID: 2, Name: "A"}, {ID: 3, Name: "B"}, {ID: 4, Name: "C"}}
	tests := []struct {
		name      string
		positions []positionConstraint
		deadlines []deadlineConstraint
		count     int
	}{
		{name: "unconstrained", count: 24},
		{name: "fixed name", positions: []positionConstraint{{Index: 0, Name: "A"}}, count: 12},
		{name: "fixed object", positions: []positionConstraint{{Index: 0, Obj: 1}}, count: 6},
		{name: "deadline", deadlines: []deadlineConstraint{{Through: 1, Name: "A", Count: 1}}, count: 12},
		{name: "nested deadlines", deadlines: []deadlineConstraint{{Through: 1, Name: "A", Count: 1}, {Through: 3, Name: "B", Count: 1}}, count: 8},
		{name: "object and deadline", positions: []positionConstraint{{Index: 0, Obj: 1}}, deadlines: []deadlineConstraint{{Through: 2, Name: "B", Count: 1}}, count: 2},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			seen := make(map[string]bool)
			for rank := 0; rank < tc.count; rank++ {
				order, logWeight, compatible, err := sampleConstrainedPermutation(cards, tc.positions, tc.deadlines, &rankRandom{value: uint64(rank)})
				if err != nil {
					t.Fatal(err)
				}
				if !compatible {
					t.Fatal("compatible constraints rejected")
				}
				if math.Abs(math.Exp(logWeight)-float64(tc.count)/24) > 1e-12 {
					t.Fatalf("weight=%g want %g", math.Exp(logWeight), float64(tc.count)/24)
				}
				if !permutationSatisfies(order, cards, tc.positions, tc.deadlines) {
					t.Fatalf("order %v violates constraints", order)
				}
				seen[keyIDs(order)] = true
			}
			if len(seen) != tc.count {
				t.Fatalf("sampled %d unique orders, want %d", len(seen), tc.count)
			}
		})
	}
}

func TestConstrainedPermutationRejectsInvalidOrImpossibleConstraints(t *testing.T) {
	cards := []proposalCard{{ID: 1, Name: "A"}, {ID: 2, Name: "B"}, {ID: 3, Name: "C"}}
	invalid := []struct {
		positions []positionConstraint
		deadlines []deadlineConstraint
	}{
		{positions: []positionConstraint{{Index: -1, Name: "A"}}},
		{positions: []positionConstraint{{Index: 3, Name: "A"}}},
		{positions: []positionConstraint{{Index: 0, Obj: 99}}},
		{positions: []positionConstraint{{Index: 0, Obj: 1}, {Index: 1, Obj: 1}}},
		{positions: []positionConstraint{{Index: 0, Name: "A"}, {Index: 0, Name: "B"}}},
		{deadlines: []deadlineConstraint{{Through: 4, Name: "A", Count: 1}}},
		{deadlines: []deadlineConstraint{{Through: 1, Name: "A", Count: -1}}},
	}
	for _, tc := range invalid {
		if _, _, _, err := sampleConstrainedPermutation(cards, tc.positions, tc.deadlines, &rankRandom{}); err == nil {
			t.Fatalf("accepted invalid positions=%v deadlines=%v", tc.positions, tc.deadlines)
		}
	}
	_, _, compatible, err := sampleConstrainedPermutation(cards, nil, []deadlineConstraint{{Through: 1, Name: "A", Count: 2}}, &rankRandom{})
	if err != nil || compatible {
		t.Fatalf("impossible constraints: compatible=%v err=%v", compatible, err)
	}
}

func permutationSatisfies(order []state.ObjID, cards []proposalCard, positions []positionConstraint, deadlines []deadlineConstraint) bool {
	names := make(map[state.ObjID]string)
	for _, card := range cards {
		names[card.ID] = card.Name
	}
	for _, c := range positions {
		if c.Obj != 0 && order[c.Index] != c.Obj || c.Name != "" && names[order[c.Index]] != c.Name {
			return false
		}
	}
	for _, c := range deadlines {
		count := 0
		for _, id := range order[:c.Through] {
			if names[id] == c.Name {
				count++
			}
		}
		if count < c.Count {
			return false
		}
	}
	return true
}

func keyIDs(ids []state.ObjID) string {
	return fmt.Sprint(ids)
}

func TestConstrainedPermutationDoesNotMutateInput(t *testing.T) {
	cards := []proposalCard{{ID: 1, Name: "A"}, {ID: 2, Name: "A"}, {ID: 3, Name: "B"}}
	want := append([]proposalCard(nil), cards...)
	_, _, _, err := sampleConstrainedPermutation(cards, []positionConstraint{{Index: 0, Name: "A"}}, nil, &rankRandom{})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cards, want) {
		t.Fatalf("mutated cards: %v", cards)
	}
}

func TestConstraintCounterPrecomputesFactorials(t *testing.T) {
	cards := []proposalCard{{ID: 1, Name: "A"}, {ID: 2, Name: "A"}, {ID: 3, Name: "B"}, {ID: 4, Name: "C"}, {ID: 5, Name: "D"}}
	counter, _, err := newConstraintCounter(cards, nil, []deadlineConstraint{{Through: 2, Name: "A", Count: 1}})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"1", "1", "2", "6", "24", "120"}
	if len(counter.factorials) != len(want) {
		t.Fatalf("factorial table length = %d, want %d", len(counter.factorials), len(want))
	}
	for i, value := range counter.factorials {
		if value.String() != want[i] {
			t.Fatalf("factorial[%d] = %s, want %s", i, value, want[i])
		}
		if i > 0 && value == counter.factorials[i-1] {
			t.Fatalf("factorial[%d] aliases factorial[%d]", i, i-1)
		}
	}
}

func TestConstraintCounterMemoReturnsImmutableEntry(t *testing.T) {
	cards := []proposalCard{{ID: 1, Name: "A"}, {ID: 2, Name: "A"}, {ID: 3, Name: "B"}, {ID: 4, Name: "C"}}
	counter, available, err := newConstraintCounter(cards, nil, []deadlineConstraint{{Through: 2, Name: "A", Count: 1}})
	if err != nil {
		t.Fatal(err)
	}
	remaining := append([]int(nil), counter.initialFree...)
	wantRemaining := append([]int(nil), remaining...)
	other := len(available) - sumInts(remaining)
	first := counter.count(0, remaining, append([]int(nil), counter.initialUnseen...), other)
	if !reflect.DeepEqual(remaining, wantRemaining) {
		t.Fatalf("count mutated remaining: got %v want %v", remaining, wantRemaining)
	}
	second := counter.count(0, remaining, append([]int(nil), counter.initialUnseen...), other)
	if first != second {
		t.Fatal("memo hit returned a defensive big.Int copy instead of the immutable cached entry")
	}
	if first.String() != "20" {
		t.Fatalf("count = %s, want 20", first)
	}
}

func TestConstraintCounterSparseDeadline(t *testing.T) {
	cards := make([]proposalCard, 20)
	for i := range cards {
		cards[i] = proposalCard{ID: state.ObjID(i + 1), Name: "B"}
	}
	cards[0].Name = "A"
	counter, available, err := newConstraintCounter(cards, nil, []deadlineConstraint{{Through: 7, Name: "A", Count: 1}})
	if err != nil {
		t.Fatal(err)
	}
	got := counter.total(available)
	want := big.NewInt(1)
	for i := int64(2); i <= 19; i++ {
		want.Mul(want, big.NewInt(i))
	}
	want.Mul(want, big.NewInt(7))
	if got.Cmp(want) != 0 {
		t.Fatalf("count = %s, want %s", got, want)
	}
}

func TestConstraintCounterPastPackedKeyBoundFallsBack(t *testing.T) {
	// A problem with more distinct names than the packed key holds must not be
	// rejected: it takes the string-key path. Every position is pinned, so the
	// count is 1 and the walk stays cheap despite the wide name list.
	n := countKeyMaxNames + 2
	cards := make([]proposalCard, n)
	positions := make([]positionConstraint, n)
	for i := 0; i < n; i++ {
		cards[i] = proposalCard{ID: state.ObjID(i + 1), Name: fmt.Sprintf("N%02d", i)}
		positions[i] = positionConstraint{Index: i, Obj: state.ObjID(i + 1)}
	}
	counter, available, err := newConstraintCounter(cards, positions, nil)
	if err != nil {
		t.Fatalf("counter with %d distinct names rejected: %v", n, err)
	}
	if len(counter.names) <= countKeyMaxNames {
		t.Fatalf("test needs more than %d names, got %d", countKeyMaxNames, len(counter.names))
	}
	if _, ok := countKey(0, make([]int, len(counter.names)), make([]int, len(counter.names)), 0); ok {
		t.Fatalf("countKey packed a %d-name problem past its %d bound", len(counter.names), countKeyMaxNames)
	}
	if got := counter.total(available); got.Cmp(big.NewInt(1)) != 0 {
		t.Fatalf("count = %s, want 1", got)
	}
}

func TestCountKeyIsUnambiguousWithNoAllocation(t *testing.T) {
	// The packed key is comparable and allocation-free; the property the
	// string form held (four distinct states produce four distinct keys) is
	// what makes the memo sound, and a collision here would silently return
	// another position's count.
	cases := [][4]any{
		{1, []int{23, 4}, []int{0}, 5},
		{12, []int{3, 4}, []int{0}, 5},
		{1, []int{2, 34}, []int{0}, 5},
		{1, []int{23, 4}, []int{0}, 6},
	}
	keys := make([]countKeyT, len(cases))
	for i, c := range cases {
		k, ok := countKey(c[0].(int), c[1].([]int), c[2].([]int), c[3].(int))
		if !ok {
			t.Fatalf("case %d did not pack", i)
		}
		keys[i] = k
	}
	for i := range keys {
		for j := i + 1; j < len(keys); j++ {
			if keys[i] == keys[j] {
				t.Fatalf("keys %d and %d collide", i, j)
			}
		}
	}
	if allocs := testing.AllocsPerRun(100, func() {
		_, _ = countKey(12, []int{3, 4, 5, 6, 7}, []int{0}, 8)
	}); allocs != 0 {
		t.Fatalf("countKey allocations = %.0f, want 0", allocs)
	}
}

// TestCountKeyPackedAndStringFormsAgree holds the packed key to the string
// fallback: the two must never disagree on distinctness, or a problem that
// crosses countKeyMaxNames would memoise differently from one under it.
func TestCountKeyPackedAndStringFormsAgree(t *testing.T) {
	samples := []struct {
		pos               int
		remaining, unseen []int
		other             int
	}{
		{0, []int{1}, []int{0}, 0},
		{3, []int{2, 2, 1}, []int{1, 0, 0}, 7},
		{60, []int{1, 0}, []int{0, 0}, 65535},
	}
	for i, s := range samples {
		k, ok := countKey(s.pos, s.remaining, s.unseen, s.other)
		if !ok {
			t.Fatalf("sample %d did not pack", i)
		}
		str := countKeyStr(s.pos, s.remaining, s.unseen, s.other)
		if str == "" {
			t.Fatalf("sample %d: empty string key", i)
		}
		for j, o := range samples {
			if i == j {
				continue
			}
			ko, _ := countKey(o.pos, o.remaining, o.unseen, o.other)
			if (k == ko) != (str == countKeyStr(o.pos, o.remaining, o.unseen, o.other)) {
				t.Fatalf("samples %d and %d: packed and string forms disagree", i, j)
			}
		}
	}
}

func TestConstrainedPermutationAllocationBudget(t *testing.T) {
	cards, deadlines := constrainedPermutationBenchmarkFixture()
	allocs := testing.AllocsPerRun(1, func() {
		if _, _, compatible, err := sampleConstrainedPermutation(cards, nil, deadlines, &rankRandom{}); err != nil || !compatible {
			t.Fatalf("sample: compatible=%v err=%v", compatible, err)
		}
	})
	if allocs > 300000 {
		t.Fatalf("allocations = %.0f, want <= 300000", allocs)
	}
}

func TestConstraintPlanCacheReusesOnlyIdenticalInputs(t *testing.T) {
	cards := []proposalCard{{ID: 1, Name: "A"}, {ID: 2, Name: "A"}, {ID: 3, Name: "B"}, {ID: 4, Name: "C"}}
	positions := []positionConstraint{{Index: 0, Name: "A"}}
	deadlines := []deadlineConstraint{{Through: 2, Name: "B", Count: 1}}
	cache := newConstraintPlanCache()

	first, err := cache.get(cards, positions, deadlines)
	if err != nil {
		t.Fatal(err)
	}
	second, err := cache.get(append([]proposalCard(nil), cards...), append([]positionConstraint(nil), positions...), append([]deadlineConstraint(nil), deadlines...))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("identical public proposal inputs did not reuse their compiled plan")
	}

	changedCards := append([]proposalCard(nil), cards...)
	changedCards[0].ID = 99
	differentID, err := cache.get(changedCards, positions, deadlines)
	if err != nil {
		t.Fatal(err)
	}
	if differentID == first {
		t.Fatal("different physical IDs reused a compiled plan")
	}
	changedDeadlines := append([]deadlineConstraint(nil), deadlines...)
	changedDeadlines[0].Count++
	differentDeadline, err := cache.get(cards, positions, changedDeadlines)
	if err != nil {
		t.Fatal(err)
	}
	if differentDeadline == first {
		t.Fatal("different deadlines reused a compiled plan")
	}
}

func BenchmarkConstrainedPermutation60(b *testing.B) {
	cards, deadlines := constrainedPermutationBenchmarkFixture()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, _, compatible, err := sampleConstrainedPermutation(cards, nil, deadlines, &rankRandom{}); err != nil || !compatible {
			b.Fatalf("sample: compatible=%v err=%v", compatible, err)
		}
	}
}

func BenchmarkConstrainedPermutationPlanReuse60(b *testing.B) {
	cards, deadlines := constrainedPermutationBenchmarkFixture()
	cache := newConstraintPlanCache()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		plan, err := cache.get(cards, nil, deadlines)
		if err != nil {
			b.Fatal(err)
		}
		if _, _, compatible, err := plan.sample(&rankRandom{value: uint64(i)}); err != nil || !compatible {
			b.Fatalf("sample: compatible=%v err=%v", compatible, err)
		}
	}
}

func constrainedPermutationBenchmarkFixture() ([]proposalCard, []deadlineConstraint) {
	cards := make([]proposalCard, 60)
	for i := range cards {
		name := "other"
		if i < 20 {
			name = string(rune('A' + i/4))
		}
		cards[i] = proposalCard{ID: state.ObjID(i + 1), Name: name}
	}
	deadlines := []deadlineConstraint{
		{Through: 8, Name: "A", Count: 1},
		{Through: 12, Name: "B", Count: 1},
		{Through: 16, Name: "C", Count: 1},
		{Through: 20, Name: "D", Count: 1},
		{Through: 24, Name: "E", Count: 1},
	}
	return cards, deadlines
}
