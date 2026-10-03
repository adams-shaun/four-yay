package effects

import (
	"unsafe"

	"github.com/adams-shaun/gorge/cards"
)

// SAFacts is the one per-ability compiled facts record (W4 steps 1 and 3 of
// docs/superpowers/specs/2026-10-03-rules-engine-lasagna-design.md, section
// 8). An ability has exactly one downstream slot (cards.ExtSlot) and a second
// claim on it fails silently, so every compiled per-ability fact lives in this
// one struct hung on the slot and nothing competes for it: the per-API typed
// parameter structs (ChangeZone today) and the rules tier's private half (the
// mana walk's gate facts, opaque here).
//
// The record is defined in effects, not rules, because resolution reads it
// and effects sits below rules; rules owns its configured binding (every
// ability reachable from a configured face gets one when the engine's
// compiled text is built, TestEveryConfiguredAbilityHasItsFactsRecord) and
// hangs its own half on Rules.
//
// A record is immutable once published and a pure function of the ability's
// text. Two keys guard a by-value SA copy that shares its original's slot:
// the rules half answers only for SA itself (pointer identity), and each typed
// parameter struct answers only for the exact Params map it was compiled from
// (ParamSet's identity rule), so a cards.ResolveSVar copy -- a fresh SA
// sharing its template's parse -- reads its template's typed params through
// the typed reader's front cache (changezone_params.go), while a copy whose
// Params were rewritten recompiles.
type SAFacts struct {
	// SA is the ability the configuring engine built the record for.
	SA *cards.SA
	// ChangeZone is api:ChangeZone's compiled parameter set (changezone_params.go),
	// non-nil exactly when the ability's API is ChangeZone.
	ChangeZone *ChangeZoneParams
	// Rules is the rules tier's private half, opaque here: the mana walk's
	// gate facts for an AB$ ability, nil otherwise.
	Rules unsafe.Pointer
}

// NewSAFacts compiles sa's typed halves into a fresh record naming sa. The
// caller (rules' configured binding) adds its own half and publishes it.
func NewSAFacts(sa *cards.SA) *SAFacts {
	f := &SAFacts{SA: sa}
	if isChangeZoneSA(sa) {
		f.ChangeZone = compileChangeZone(sa)
	}
	return f
}

// Publish hangs f on its ability's slot for a pointer read; the first
// publisher wins (every publisher computes the same facts from the same text).
func (f *SAFacts) Publish() {
	if f.SA != nil {
		f.SA.ExtSlot().Store(unsafe.Pointer(f))
	}
}

// LoadSAFacts returns the record published on sa's slot, whoever it was built
// for (callers apply their own identity check), or nil.
func LoadSAFacts(sa *cards.SA) *SAFacts {
	if sa == nil {
		return nil
	}
	return (*SAFacts)(sa.ExtSlot().Load())
}
