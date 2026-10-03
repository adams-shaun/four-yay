package cost

import "testing"

var costTokensSink []string

var parsedCostSink Cost

// A warm parse can allocate its token storage, but must not rebuild the
// immutable brace-normalization table on every offer-time cost check.
func TestParseCostReusesNormalization(t *testing.T) {
	for _, tc := range []struct {
		src string
		max float64
	}{
		{"U", 2},
		// Replacer owns both a byte buffer and the normalized string;
		// splitCostTokens owns a token buffer and a one-element slice.
		{"{U}", 4},
	} {
		t.Run(tc.src, func(t *testing.T) {
			allocs := testing.AllocsPerRun(100, func() {
				c := ParseCost(tc.src)
				if c.Colored[1] != 1 || c.CMC() != 1 {
					t.Fatalf("ParseCost(%q) = %+v; want one blue", tc.src, c)
				}
			})
			if allocs > tc.max {
				t.Fatalf("ParseCost(%q) allocated %.0f objects; budget %.0f", tc.src, allocs, tc.max)
			}
		})
	}
}

func BenchmarkSplitCostTokens(b *testing.B) {
	const raw = "2 W U Sac<1/Artifact;Creature/artifact or creature> PayLife<2>\tT"
	want := []string{"2", "W", "U", "Sac<1/Artifact;Creature/artifact or creature>", "PayLife<2>", "T"}
	b.ReportAllocs()
	for range b.N {
		costTokensSink = splitCostTokens(raw)
	}
	if len(costTokensSink) != len(want) {
		b.Fatalf("tokens = %v, want %v", costTokensSink, want)
	}
	for i := range want {
		if costTokensSink[i] != want[i] {
			b.Fatalf("tokens = %v, want %v", costTokensSink, want)
		}
	}
}

func BenchmarkParseCostHotShapes(b *testing.B) {
	costs := []string{"U", "1 G", "2 W U", "T", "3", "Sac<1/Creature>", "PayLife<2> B"}
	b.ReportAllocs()
	for i := range b.N {
		parsedCostSink = ParseCost(costs[i%len(costs)])
	}
	if parsedCostSink.CMC() < 0 {
		b.Fatal("impossible negative cost digest")
	}
}

func BenchmarkParseCostMana(b *testing.B) {
	b.ReportAllocs()
	for range b.N {
		parsedCostSink = ParseCost("2 U U")
	}
}

func BenchmarkParseCostHybridNonMana(b *testing.B) {
	b.ReportAllocs()
	for range b.N {
		parsedCostSink = ParseCost("GWP 2B Sac<1/Creature>")
	}
}

func BenchmarkParseCostGraveyardLife(b *testing.B) {
	b.ReportAllocs()
	for range b.N {
		parsedCostSink = ParseCost("1 B ExileFromGrave<1/CARDNAME> PayLife<2>")
	}
}

func TestParseCostSingleManaDoesNotAllocate(t *testing.T) {
	if got := ParseCost("1 U"); got.Generic != 1 || got.Colored[1] != 1 {
		t.Fatalf("ParseCost = %+v", got)
	}
	if allocs := testing.AllocsPerRun(1000, func() { ParseCost("1 U") }); allocs != 0 {
		t.Fatalf("ParseCost allocated %.2f objects for plain mana, want zero", allocs)
	}
}

func BenchmarkParseUnlessCostBraced(b *testing.B) {
	b.ReportAllocs()
	var ok bool
	for range b.N {
		parsedCostSink, ok = ParseUnlessCost("{2}{U}{B}")
	}
	if !ok || parsedCostSink.Generic != 2 || parsedCostSink.Colored[1] != 1 || parsedCostSink.Colored[2] != 1 {
		b.Fatalf("strict cost = %+v, ok=%v", parsedCostSink, ok)
	}
}

func TestParseUnlessCostReusesBraceNormalizer(t *testing.T) {
	if got, ok := ParseUnlessCost("{2}{U}{B}"); !ok || got.Generic != 2 || got.Colored[1] != 1 || got.Colored[2] != 1 {
		t.Fatalf("ParseUnlessCost = %+v, %v", got, ok)
	}
	if allocs := testing.AllocsPerRun(1000, func() { ParseUnlessCost("{2}{U}{B}") }); allocs > 2 {
		t.Fatalf("ParseUnlessCost allocated %.2f objects, want only normalized output", allocs)
	}
}
