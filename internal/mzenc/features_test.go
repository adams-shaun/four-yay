package mzenc

import "testing"

func TestIndexForMirrorsJavaSignedLong(t *testing.T) {
	minI64 := uint64(1) << 63 // math.MinInt64 bit pattern, via a runtime conversion
	cases := []struct {
		h    uint64
		want int32
	}{
		{0, 0},
		{1, 1},
		{minI64, int32(int64(minI64) % defaultTable)}, // Java long overflow, same lattice
		{0xFFFFFFFFFFFFFFFF, int32(int64(1) % defaultTable)},
	}
	for _, c := range cases {
		if got := indexFor(c.h, defaultTable); got != c.want {
			t.Fatalf("indexFor(%#x)=%d want %d", c.h, got, c.want)
		}
	}
	if globalSeed != 0x9E3779B185EBCA87 {
		t.Fatalf("globalSeed bit pattern changed")
	}
}

func TestOccurrenceCardinalityIsDistinctIDs(t *testing.T) {
	e := NewEncoder(defaultTable)
	e.Root().AddFeature("Card")
	e.Root().AddFeature("Card")
	if len(e.IDs()) != 2 {
		t.Fatalf("two repeats of one name must hash Card#1 and Card#2, got %d ids", len(e.IDs()))
	}
}

func TestNumericThermometerEmitsBreakpointsAndLowCounters(t *testing.T) {
	e := NewEncoder(defaultTable)
	e.Root().AddNumericFeature("Power", 50, true)
	// 50 >= 32 -> Power@32; then Power@0..@19 (20 ids). Total 21.
	if len(e.IDs()) != 21 {
		t.Fatalf("Power=50: want 21 ids, got %d", len(e.IDs()))
	}
	e2 := NewEncoder(defaultTable)
	e2.Root().AddNumericFeature("Power", 0, true) // 0 < 32, then no 0..n loop -> no ids
	if len(e2.IDs()) != 0 {
		t.Fatalf("Power=0: want 0 ids, got %d", len(e2.IDs()))
	}
}

func TestSubFeatureKeyedByOccurrenceAndReusedAfterRefresh(t *testing.T) {
	e := NewEncoder(defaultTable)
	a := e.Root().SubFeatures("X", true)
	b := e.Root().SubFeatures("X", true)
	if a == b {
		t.Fatalf("second SubFeatures call in one state must create X#2, a distinct node")
	}
	e.Root().StateRefresh()
	c := e.Root().SubFeatures("X", true)
	if c != a {
		t.Fatalf("after StateRefresh the X#1 node must be reused")
	}
}

// TestEncoderIsDeterministic guards the pure-function contract: the id set is
// a function of the op sequence alone. The repo-wide no-clock/no-rand rule is
// enforced by internal/archtest.
func TestEncoderIsDeterministic(t *testing.T) {
	build := func() map[int32]struct{} {
		e := NewEncoder(defaultTable)
		e.Root().AddNumericFeature("Power", 7, true)
		e.Root().SubFeatures("Hand", true).AddFeature("Card")
		return e.IDs()
	}
	a, b := build(), build()
	if len(a) != len(b) {
		t.Fatalf("nondeterministic id count: %d vs %d", len(a), len(b))
	}
	for id := range a {
		if _, ok := b[id]; !ok {
			t.Fatalf("nondeterministic: id %d missing on the second run", id)
		}
	}
}
