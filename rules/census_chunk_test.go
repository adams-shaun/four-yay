package rules

import (
	"go/ast"
	"go/parser"
	"go/token"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// censusSubjects returns a per-process memo of the corpus cards keep admits,
// sorted by front-face name: the sharded class censuses (one test per chunk,
// the operator's per-test budget) share one subject walk over the corpus
// instead of each chunk re-filtering ~33k cards.
func censusSubjects(keep func(*cards.Card) bool) func(t testing.TB) []*cards.Card {
	var once sync.Once
	var subjects []*cards.Card
	return func(t testing.TB) []*cards.Card {
		reg := testutil.CorpusRegistry(t)
		once.Do(func() {
			for _, c := range reg.AllCards() {
				if keep(c) {
					subjects = append(subjects, c)
				}
			}
			slices.SortFunc(subjects, func(a, b *cards.Card) int { return strings.Compare(a.Faces[0].Name, b.Faces[0].Name) })
		})
		return subjects
	}
}

// censusChunkRange is chunk k's half-open slice [lo, hi) of n subjects split
// into chunks contiguous, near-equal parts.
func censusChunkRange(n, k, chunks int) (lo, hi int) {
	return n * k / chunks, n * (k + 1) / chunks
}

// TestCensusChunksCoverEverySubject holds each sharded census's chunk tests
// in step with its chunk constant: exactly one test function per chunk index
// 0..chunks-1, so a chunk added to the constant but not declared (or the
// reverse) cannot silently drop a slice of the subjects. It also checks that
// censusChunkRange tiles [0, n) for every census size.
func TestCensusChunksCoverEverySubject(t *testing.T) {
	t.Parallel()
	// Each census's chunk tests live in its own file (a chunk declared
	// anywhere else reads as missing, the loud direction).
	sharded := map[string]struct {
		file   string
		chunks int
	}{
		"TestCostStaticPlannedCastsNeverCostChange":    {"cost_static_plan_census_test.go", costStaticPlanChunks},
		"TestChainTargetOfferCensusAgreesWithCastFlow": {"chain_target_census_test.go", chainTargetCensusChunks},
	}
	seen := map[string]map[int]bool{}
	for prefix, sh := range sharded {
		f, err := parser.ParseFile(token.NewFileSet(), sh.file, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		seen[prefix] = map[int]bool{}
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Recv != nil {
				continue
			}
			rest, ok := strings.CutPrefix(fn.Name.Name, prefix)
			if !ok {
				continue
			}
			k, err := strconv.Atoi(rest)
			if err != nil {
				t.Errorf("%s: a %s test must be named %s<chunk>", fn.Name.Name, prefix, prefix)
				continue
			}
			seen[prefix][k] = true
		}
	}
	for prefix, sh := range sharded {
		chunks := sh.chunks
		for k := 0; k < chunks; k++ {
			if !seen[prefix][k] {
				t.Errorf("%s%d is missing: %d chunks declared", prefix, k, chunks)
			}
		}
		if len(seen[prefix]) != chunks {
			t.Errorf("%s: %d chunk tests for %d declared chunks", prefix, len(seen[prefix]), chunks)
		}
	}
	for n := 0; n < 50; n++ {
		for chunks := 1; chunks < 9; chunks++ {
			next := 0
			for k := 0; k < chunks; k++ {
				lo, hi := censusChunkRange(n, k, chunks)
				if lo != next || hi < lo {
					t.Fatalf("censusChunkRange(%d, %d, %d) = [%d,%d), want to start at %d", n, k, chunks, lo, hi, next)
				}
				next = hi
			}
			if next != n {
				t.Fatalf("censusChunkRange over %d chunks ends at %d, want %d", chunks, next, n)
			}
		}
	}
}
