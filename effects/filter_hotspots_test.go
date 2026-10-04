package effects

import "testing"

var benchmarkMatch bool
var benchmarkCMC int32

var benchmarkPredicatePrograms = CompilePredicatePrograms([]string{
	"Creature.YouCtrl+untapped",
	"Land,Artifact",
	"Creature.UnknownPredicate",
})

// Simple filter matching is read-only and needs no owned result storage.
// Reintroducing per-alternative or per-predicate slices breaks this budget,
// including on failed alternatives and fail-closed unknown predicates.
func TestSimpleFilterMatchingDoesNotAllocate(t *testing.T) {
	g, ids := board(t)
	for _, tc := range []struct {
		spec string
		want bool
	}{
		{"Creature", true},
		{"Creature.YouCtrl+nonLegendary", true},
		{"Land,Creature.YouCtrl", true},
		{"Land,Artifact", false},
		{"Creature.UnknownPredicate", false},
		{"Creature.UnknownPredicate,Creature.YouCtrl", true},
		{"Creature.YouCtrl+", true},
		{"", false},
	} {
		t.Run(tc.spec, func(t *testing.T) {
			allocs := allocsPerRun(100, func() {
				if got := MatchesSpec(g, tc.spec, ids["myBear"], 0); got != tc.want {
					t.Fatalf("MatchesSpec(%q) = %v; want %v", tc.spec, got, tc.want)
				}
			})
			if allocs != 0 {
				t.Fatalf("MatchesSpec(%q) allocated %.0f objects; want zero", tc.spec, allocs)
			}
		})
	}
}

// parseCMC runs while evaluating ManaValue predicates. Its braced input still
// needs an output string and token slice, but rebuilding the immutable brace
// normalizer on every evaluation is avoidable hot-path work.
func TestParseCMCReusesBraceNormalizer(t *testing.T) {
	if got := parseCMC("{2}{U}{B}"); got != 4 {
		t.Fatalf("parseCMC = %d, want 4", got)
	}
	if allocs := allocsPerRun(1000, func() { parseCMC("{2}{U}{B}") }); allocs > 7 {
		t.Fatalf("parseCMC allocated %.2f objects, want no rebuilt brace normalizer", allocs)
	}
}

func BenchmarkFilterTextualMatch(b *testing.B) {
	g, ids := board(b)
	sc := SpecContext{You: 0, Source: ids["myBear"]}
	b.ReportAllocs()
	for range b.N {
		benchmarkMatch = MatchesSpecCtx(g, "Creature.YouCtrl+untapped", ids["myBear"], sc)
	}
}

func BenchmarkParseCMCBraced(b *testing.B) {
	b.ReportAllocs()
	for range b.N {
		benchmarkCMC = parseCMC("{2}{U}{B}")
	}
}

func BenchmarkFilterTextualReject(b *testing.B) {
	g, ids := board(b)
	sc := SpecContext{You: 0, Source: ids["myBear"]}
	b.ReportAllocs()
	for range b.N {
		benchmarkMatch = MatchesSpecCtx(g, "Land,Artifact", ids["myBear"], sc)
	}
}

func BenchmarkFilterTextualUnknown(b *testing.B) {
	g, ids := board(b)
	sc := SpecContext{You: 0, Source: ids["myBear"]}
	b.ReportAllocs()
	for range b.N {
		benchmarkMatch = MatchesSpecCtx(g, "Creature.UnknownPredicate", ids["myBear"], sc)
	}
}

func BenchmarkFilterCompiledYes(b *testing.B) {
	g, ids := board(b)
	sc := SpecContext{You: 0, Source: ids["myBear"]}
	b.ReportAllocs()
	for range b.N {
		benchmarkMatch = benchmarkPredicatePrograms.Evaluate("Creature.YouCtrl+untapped", g, g.Obj(ids["myBear"]), sc) == PredicateYes
	}
}

func BenchmarkFilterCompiledNo(b *testing.B) {
	g, ids := board(b)
	sc := SpecContext{You: 0, Source: ids["myBear"]}
	b.ReportAllocs()
	for range b.N {
		benchmarkMatch = benchmarkPredicatePrograms.Evaluate("Land,Artifact", g, g.Obj(ids["myBear"]), sc) == PredicateYes
	}
}

func BenchmarkFilterCompiledMaybe(b *testing.B) {
	g, ids := board(b)
	sc := SpecContext{You: 0, Source: ids["myBear"], PredicatePrograms: benchmarkPredicatePrograms}
	b.ReportAllocs()
	for range b.N {
		benchmarkMatch = MatchesSpecCtx(g, "Creature.UnknownPredicate", ids["myBear"], sc)
	}
}
