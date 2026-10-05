package rules

// CR 702.147 Decayed: "This creature can't block" plus "When this creature
// attacks, sacrifice it at end of combat." The behaviour is implemented
// rules-side, reading the DERIVED keyword list so a printed K:Decayed, a
// decayed counter (CR 122.1b, cards.CounterKeyword), a KW$ Decayed Pump grant
// and a layer-6 AddKeyword$ Decayed all behave alike:
//
//   - the can't-block half is one arm in combat.ParseHiddenKeyword
//     (rules/combat/restrictions.go), the single home every combat
//     restriction keyword text reaches through DerivedHiddenFlags;
//   - the sacrifice half is the Decayed arm of
//     checkGrantedAttackKeywordTriggers (rules/trigger_granted.go), which arms
//     the one-shot __kwDecayedSacrifice delayed registration at EndCombat.
//
// This file only carries the coverage marker, exactly as blitz.go and
// unearth.go register theirs. Proof: the real-corpus tests in
// rules/decayed_test.go.

import "github.com/adams-shaun/gorge/effects"

func init() {
	// The coverage census: Decayed (CR 702.147) is implemented rules-side, so
	// it registers here rather than through a cards/kw_*.go expander.
	effects.RegisterNonAPI("kw:Decayed")
}
