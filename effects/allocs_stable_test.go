package effects

import (
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
)

// allocsPerRun is testing.AllocsPerRun taken as the minimum of three
// measurements. The compiled-parameter front caches (cloneFront, voteFront,
// ...) are direct-mapped package globals, so a concurrent compile landing on
// a primed slot evicts it once; that does not repeat three times running. A
// collision between two abilities the measured closure itself reads DOES
// repeat on every call, which is why the configured-record half must be a
// slotted ability (slottedSA) that never reaches the front cache.
func allocsPerRun(runs int, f func()) float64 {
	best := testing.AllocsPerRun(runs, f)
	for i := 0; i < 2 && best != 0; i++ {
		if n := testing.AllocsPerRun(runs, f); n < best {
			best = n
		}
	}
	return best
}

// slottedSA is a one-ability inline card's spell ability with exactly
// params: a derived ability, so it carries the facts slot a configured
// record is published on. A hand-built &cards.SA{} has a nil ExtSlot, so
// Publish on it is a no-op and the "configured record" reader falls through
// to the direct-mapped front cache instead -- where it shares a slot with
// the test's cached ability one run in 1024 and the two evict each other on
// every lookup (the allocation-free tests' load-independent flake).
func slottedSA(t *testing.T, api string, params map[string]string) *cards.SA {
	t.Helper()
	var b strings.Builder
	b.WriteString("Name:Facts Slot Probe\nManaCost:R\nTypes:Sorcery\nA:SP$ " + api)
	for _, k := range slices.Sorted(maps.Keys(params)) {
		b.WriteString(" | " + k + "$ " + params[k])
	}
	b.WriteString("\nOracle:x\n")
	card, diags := cards.ParseBytes("inline-facts-slot.txt", []byte(b.String()))
	if len(diags) != 0 {
		t.Fatalf("fixture parse: %v", diags)
	}
	sa := card.Faces[0].Abilities[0]
	if sa.ExtSlot() == nil || sa.API != api || !maps.Equal(sa.Params, params) {
		t.Fatalf("precondition: derived %s ability with a facts slot and params %v, got %+v", api, params, sa)
	}
	return sa
}
