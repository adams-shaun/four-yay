package rules

import (
	"fmt"

	"github.com/adams-shaun/gorge/state"
)

// activeSummary is a per-build digest of active()'s list for the mana walk
// (appendAvailableManaAbilitiesGate) and grantedAbilities, both of which
// run once per object per offer walk and each re-scanned the whole list --
// copying every ~1 KB ContinuousEffect by value -- to learn two board-wide
// facts:
//
//   - hasLType: some entry is a layer-4 type effect (the gate's CR 305.6
//     granted-intrinsic block runs only then);
//   - hasGrants: some entry carries AddAbilities or GainedFaces (without one
//     grantedAbilities returns nil for every object).
//
// The key is the list itself: active()'s build count (activeBuildSeq, which
// every rebuild and every explicit retirement moves) plus the returned
// slice's backing pointer and length. A rebuild rewrites activeBuf only after
// moving the count, and the re-entrant and verify rebuilds build into fresh
// backing, so an unchanged key names an unchanged list. A clone starts with a
// zero summary (never copied, like activeBuf). In the rules test binary
// activeSummaryVerify recomputes every hit and panics on a difference.
type activeSummary struct {
	valid     bool
	seq       uint64
	base      *state.ContinuousEffect
	n         int
	hasLType  bool
	hasGrants bool
	// hasManaConvert: some entry is an Effect-delivered ManaConvert static
	// (CostStaticMode ManaConvert, manaConversionParts' registry arm).
	hasManaConvert bool
	// hasLColor: some entry is a layer-5 colour effect (layer5colors.go's
	// derived-colour table is empty without one).
	hasLColor bool
	// hasRemoveAbilities: some entry is a layer-6 "loses all abilities"
	// effect (abilityloss.go's gates answer "no loss" without one).
	hasRemoveAbilities bool
}

// activeSummaryVerify: see derivedMemoVerify. Set by the rules test binary.
var activeSummaryVerify = derivedMemoVerifyFlag != ""

// activeSummaryOf returns the digest of ces, which must be the list active()
// just returned.
func (e *Engine) activeSummaryOf(ces []ContinuousEffect) activeSummary {
	var base *state.ContinuousEffect
	if len(ces) > 0 {
		base = &ces[0]
	}
	s := &e.activeSum
	if s.valid && s.seq == e.activeBuildSeq && s.base == base && s.n == len(ces) {
		if activeSummaryVerify {
			if fresh := summarizeActive(ces); fresh.hasLType != s.hasLType || fresh.hasGrants != s.hasGrants || fresh.hasManaConvert != s.hasManaConvert ||
				fresh.hasLColor != s.hasLColor || fresh.hasRemoveAbilities != s.hasRemoveAbilities {
				panic(fmt.Sprintf("rules: active summary at build %d disagrees with a rescan (%+v vs %+v)", s.seq, *s, fresh))
			}
		}
		return *s
	}
	fresh := summarizeActive(ces)
	fresh.valid, fresh.seq, fresh.base, fresh.n = true, e.activeBuildSeq, base, len(ces)
	*s = fresh
	return fresh
}

func summarizeActive(ces []ContinuousEffect) activeSummary {
	var s activeSummary
	for i := range ces {
		ce := &ces[i]
		if ce.Layer == LType {
			s.hasLType = true
		}
		if len(ce.AddAbilities) > 0 || len(ce.GainedFaces) > 0 {
			s.hasGrants = true
		}
		if ce.CostStaticMode == "ManaConvert" {
			s.hasManaConvert = true
		}
		if ce.Layer == LColor {
			s.hasLColor = true
		}
		if ce.Layer == LAbilities && ce.RemoveAbilities {
			s.hasRemoveAbilities = true
		}
		if s.hasLType && s.hasGrants && s.hasManaConvert && s.hasLColor && s.hasRemoveAbilities {
			break
		}
	}
	return s
}
