package params

import (
	"testing"
	"unsafe"

	"github.com/adams-shaun/gorge/cards"
)

// testFacts stands in for effects.SAFacts in this package's tests: a record
// whose first field is Facts, published on the ability's slot the same way.
type testFacts struct {
	Facts
	sa *cards.SA
}

func newTestFacts(sa *cards.SA) *testFacts { return &testFacts{Facts: CompileFacts(sa), sa: sa} }

func (f *testFacts) Publish() { f.sa.ExtSlot().Store(unsafe.Pointer(f)) }

// allocsPerRun is testing.AllocsPerRun taken as the minimum of three runs
// (effects' allocs_stable_test.go).
func allocsPerRun(runs int, f func()) float64 {
	best := testing.AllocsPerRun(runs, f)
	for i := 0; i < 2 && best != 0; i++ {
		if n := testing.AllocsPerRun(runs, f); n < best {
			best = n
		}
	}
	return best
}
