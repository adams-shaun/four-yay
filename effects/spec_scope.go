package effects

import "strings"

// SpecScope is what a filter spec proves, from its text alone, about WHICH
// objects it can match relative to the spec context's You and Source. Each bit
// is a claim every match must satisfy; a spec the compiled grammar cannot read
// (an unknown base, an unknown term only narrows, but an unknown base or a
// Spell.IsTargeting shape could be anything) proves nothing, so zero is always
// safe.
type SpecScope uint8

const (
	// SpecScopeYouCtrl: every match is controlled by You.
	SpecScopeYouCtrl SpecScope = 1 << iota
	// SpecScopeNotYouCtrl: every match is controlled by someone other than You.
	SpecScopeNotYouCtrl
	// SpecScopeSelf: every match is the Source object itself.
	SpecScopeSelf
	// SpecScopeAttached: every match is the permanent Source is attached to
	// (EquippedBy / EnchantedBy / AttachedBy), and nothing when Source is not
	// an attached battlefield permanent.
	SpecScopeAttached
)

// SpecScopeOf reads spec's compiled predicate program and returns the scope
// bits that hold for EVERY alternative. It reads only the process-wide
// compiled-spec cache (compiledSpecFor): no allocation on a cached spec, no
// game state. A term the grammar does not model (predicateTerm.maybe) is
// skipped -- it is a conjunct, so it can only narrow the matches -- and an
// alternative whose base is not modelled proves nothing.
func SpecScopeOf(spec string) SpecScope {
	if spec == "" || strings.HasPrefix(spec, "EACH ") {
		return 0
	}
	cs := compiledSpecFor(spec)
	if cs == nil || len(cs.prog.alternatives) == 0 {
		return 0
	}
	all := SpecScopeYouCtrl | SpecScopeNotYouCtrl | SpecScopeSelf | SpecScopeAttached
	for i := range cs.prog.alternatives {
		alt := &cs.prog.alternatives[i]
		if alt.baseMaybe {
			return 0
		}
		var got SpecScope
		for _, t := range alt.terms {
			if t.maybe {
				continue
			}
			switch t.kind {
			case predicateTermYouCtrl:
				if t.negated {
					got |= SpecScopeNotYouCtrl
				} else {
					got |= SpecScopeYouCtrl
				}
			case predicateTermYouDontCtrl:
				if t.negated {
					got |= SpecScopeYouCtrl
				} else {
					got |= SpecScopeNotYouCtrl
				}
			case predicateTermSelf:
				if !t.negated {
					got |= SpecScopeSelf
				}
			case predicateTermAttachedBy:
				if !t.negated {
					got |= SpecScopeAttached
				}
			}
		}
		all &= got
		if all == 0 {
			return 0
		}
	}
	return all
}
