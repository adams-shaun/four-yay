package rules

import (
	"strings"
	"sync/atomic"
)

// specReadsDerived reports whether a filter spec can read any of the
// derived characteristics rules' matchesSpec binds into the SpecContext
// (ExtraKeywords and the derived/base P/T). When it cannot, binding them is
// dead work -- and binding them costs a full Derived layer walk per candidate,
// the single largest cost of the target/cost/trigger candidate walks.
//
// The test is TEXTUAL and deliberately a superset of every effects-side read
// of those fields: the keyword reads are the with<Keyword>/without<Keyword>
// predicates (keywordPredicates: every key begins "with") and the Affinity
// base (matched case-insensitively), and the P/T reads are numericPred's
// power/toughness/basePower/baseToughness fields (and their capitalised RHS
// aliases). The probe lower-cases the spec and looks for "with", "affinity",
// "power" and "toughness" ANYWHERE in the text, so a nested sub-spec (an
// IsTargeting argument, a sameName referent) is covered too: a spec that
// contains none of them never consults the bound values, and its match is
// identical whether or not they are bound. specDerivedVerify (the rules test
// binary) evaluates a skipped match both ways and panics on a difference.
//
// The answer is a pure function of the spec, cached in a direct-mapped front
// keyed by the string's data pointer and length (the specProvenanceGate
// pattern); an entry holds the string, so a hit is an exact compare.
func specReadsDerived(spec string) bool {
	slot := &specDerivedFront[provGateSlot(spec)&(1<<specDerivedBits-1)]
	if ent := slot.Load(); ent != nil && ent.spec == spec {
		return ent.reads
	}
	reads := computeSpecReadsDerived(spec)
	slot.Store(&specDerivedEntry{spec: spec, reads: reads})
	return reads
}

func computeSpecReadsDerived(spec string) bool {
	low := strings.ToLower(spec)
	return strings.Contains(low, "with") || strings.Contains(low, "affinity") ||
		strings.Contains(low, "power") || strings.Contains(low, "toughness")
}

type specDerivedEntry struct {
	spec  string
	reads bool
}

const specDerivedBits = 12

var specDerivedFront [1 << specDerivedBits]atomic.Pointer[specDerivedEntry]

// specDerivedVerify: see derivedMemoVerify. Set by the rules test binary.
var specDerivedVerify = derivedMemoVerifyFlag != ""
