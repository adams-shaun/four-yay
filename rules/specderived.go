package rules

import (
	"strings"
	"sync/atomic"

	"github.com/adams-shaun/gorge/effects"
)

// specBindFacts returns every textual bind fact matchesSpec asks of a spec,
// from the same cached front as specReadsDerived: whether it can read the
// derived keywords/P/T (reads), the layer-5 colours (colors,
// layer5colors.go's computeSpecReadsColors), the static-goad table (goaded:
// it names IsGoaded) and the greatest-power set (greatest: it names
// greatestPower, effects.GreatestPowerDerivedPTs' own gate). Each is a pure
// function of the spec, so one lookup answers all four.
func specBindFacts(spec string) *specDerivedEntry {
	slot := &specDerivedFront[provGateSlot(spec)&(1<<specDerivedBits-1)]
	if ent := slot.Load(); ent != nil && ent.spec == spec {
		return ent
	}
	return storeSpecDerived(slot, spec)
}

func storeSpecDerived(slot *atomic.Pointer[specDerivedEntry], spec string) *specDerivedEntry {
	ent := &specDerivedEntry{spec: spec, reads: computeSpecReadsDerived(spec), colors: computeSpecReadsColors(spec),
		goaded: strings.Contains(spec, "IsGoaded"), greatest: strings.Contains(spec, "greatestPower")}
	slot.Store(ent)
	return ent
}

func computeSpecReadsDerived(spec string) bool {
	low := strings.ToLower(spec)
	return strings.Contains(low, "with") || strings.Contains(low, "affinity") ||
		strings.Contains(low, "power") || strings.Contains(low, "toughness")
}

type specDerivedEntry struct {
	spec                            string
	reads, colors, goaded, greatest bool
}

const specDerivedBits = 12

var specDerivedFront [1 << specDerivedBits]atomic.Pointer[specDerivedEntry]

// specDerivedVerify: see derivedMemoVerify. Set by the rules test binary.
var specDerivedVerify = derivedMemoVerifyFlag != ""

// A derivedMemoVerifyFlag build also verifies the effects-side spec fronts.
func init() {
	if derivedMemoVerifyFlag != "" {
		effects.VerifySpecCaches = true
	}
}
