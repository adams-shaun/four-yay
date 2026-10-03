package rules

import (
	"unsafe"

	"github.com/adams-shaun/gorge/cards"
)

// saFacts is the one rules-owned per-ability facts record (W4 step 1 of
// docs/superpowers/specs/2026-10-03-rules-engine-lasagna-design.md, section
// 8). An ability has exactly one downstream slot (cards.ExtSlot) and a
// second claim on it fails silently, so every compiled per-SA fact -- the
// mana walk's gate facts today, W4's per-API typed parameter structs next --
// lives in this one struct hung on the slot, and nothing competes for it.
//
// A record is built for every configured ability (every ability reachable
// from a configured face: its Abilities and their SubAbility$ chains,
// trigger Execute$ bodies and replacement bodies, whatever its Kind) when
// the compiledText is built, and is a pure function of the ability's
// immutable text. TestEveryConfiguredAbilityHasItsFactsRecord holds the
// binding total.
type saFacts struct {
	// sa is the ability the record was computed for: a by-value SA copy
	// shares its original's published slot and must not read it.
	sa *cards.SA
	// mana is the mana walk's gate facts (mana_safacts.go): non-nil exactly
	// for an AB$ ability.
	mana *manaSAFacts
	// mayAsk caches the resolution kernel's ask-free predicate over this
	// ability and its SubAbility$ chain (resolve_mayask.go): a pure function
	// of the text, filled on first use with an atomic store.
	mayAsk uint32
}

// buildSAFacts computes ab's facts record.
func buildSAFacts(ab *cards.SA, costOf func(string) *compiledCost) *saFacts {
	f := &saFacts{sa: ab}
	if ab.Kind == "AB" {
		m := buildManaSAFactsValue(ab, costOf)
		f.mana = &m
	}
	return f
}

// publishSAFacts hangs f on its ability's slot for a pointer read; the first
// configuration to publish wins (every configuration computes the same
// facts from the same text).
func publishSAFacts(f *saFacts) {
	f.sa.ExtSlot().Store(unsafe.Pointer(f))
}

// factsOf returns sa's configured facts record, or nil for an ability
// outside the configured set (a runtime-built SA, or a by-value copy). A nil
// table (an engine built without one) has no records.
func (ct *compiledText) factsOf(sa *cards.SA) *saFacts {
	if ct == nil || sa == nil {
		return nil
	}
	// The record published on the ability itself when it is its own, else
	// the engine's table. A published record may come from another
	// configuration's compiledText, whose compiled cost is the same frozen
	// parse of the same text at a different address.
	if p := sa.ExtSlot().Load(); p != nil && (*saFacts)(p).sa == sa {
		return (*saFacts)(p)
	}
	return ct.saFacts[sa]
}
