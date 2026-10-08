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
