package rules

import (
	"unsafe"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
)

// saFacts is the one per-ability facts record (W4 steps 1 and 3 of
// docs/superpowers/specs/2026-10-03-rules-engine-lasagna-design.md, section
// 8): effects.SAFacts, defined in effects because resolution reads its typed
// parameter halves (ChangeZone's today) and effects sits below rules. An
// ability has exactly one downstream slot (cards.ExtSlot) and a second claim
// on it fails silently, so every compiled per-SA fact lives in this one
// record hung on the slot, and nothing competes for it. Rules' own half --
// the mana walk's gate facts (mana_safacts.go) for an AB$ ability -- rides
// the record's opaque Rules pointer.
//
// Rules owns the binding: a record is built for every configured ability
// (every ability reachable from a configured face: its Abilities and their
// SubAbility$ chains, trigger Execute$ bodies and replacement bodies,
// whatever its Kind) when the compiledText is built, and is a pure function
// of the ability's immutable text. TestEveryConfiguredAbilityHasItsFactsRecord
// holds the binding total.
type saFacts = effects.SAFacts

// buildSAFacts computes ab's facts record: the typed halves effects compiles,
// plus the mana half for an AB$ ability.
func buildSAFacts(ab *cards.SA, costOf func(string) *compiledCost) *saFacts {
	f := effects.NewSAFacts(ab)
	if ab.Kind == "AB" {
		m := buildManaSAFactsValue(ab, costOf)
		f.Rules = unsafe.Pointer(&m)
	}
	return f
}

// manaHalf is f's mana walk gate facts: non-nil exactly for a configured AB$
// ability's record.
func manaHalf(f *saFacts) *manaSAFacts { return (*manaSAFacts)(f.Rules) }

// publishSAFacts hangs f on its ability's slot for a pointer read; the first
// configuration to publish wins (every configuration computes the same
// facts from the same text).
func publishSAFacts(f *saFacts) { f.Publish() }

// factsOf returns sa's configured facts record, or nil for an ability
// outside the configured set (a runtime-built SA, or a by-value copy). A nil
// table (an engine built without one) has no records.
func (ct *compiledText) factsOf(sa *cards.SA) *saFacts {
	if ct == nil || sa == nil {
		return nil
	}
	// The record published on the ability itself when it is its own (a
	// lazily published typed-params record names no SA, so it never
	// matches), else the engine's table. A published record may come from
	// another configuration's compiledText, whose compiled cost is the same
	// frozen parse of the same text at a different address.
	if f := effects.LoadSAFacts(sa); f != nil && f.SA == sa {
		return f
	}
	return ct.saFacts[sa]
}
