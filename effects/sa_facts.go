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
// parameter structs (ChangeZone, ChangeZoneAll, Attach, DealDamage,
// PutCounter, Effect, DelayedTrigger, CopyPermanent, Clone, Dig, DigUntil, RemoveCounter, Token, Vote, RepeatEach) and the rules tier's private half (the
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
	// ChangeZoneAll is api:ChangeZoneAll's compiled parameter set
	// (changezoneall_params.go), non-nil exactly when the API is ChangeZoneAll.
	ChangeZoneAll *ChangeZoneAllParams
	// Attach is api:Attach's compiled parameter set (attach_params.go),
	// non-nil exactly when the API is Attach.
	Attach *AttachParams
	// Rules is the rules tier's private half, opaque here: the mana walk's
	// gate facts for an AB$ ability, nil otherwise.
	Rules unsafe.Pointer
	// MayAsk caches rules' resolution-kernel ask-free predicate over the
	// ability and its SubAbility$ chain (rules/resolve_mayask.go): a pure
	// function of the text, filled on first use with an atomic store
	// (0 unknown, 1 ask-free, 2 may ask).
	MayAsk uint32

	// W4 step 3, the Charm/Pump/Draw compilers (charm_params.go,
	// pump_params.go, draw_params.go): each is non-nil exactly when the
	// ability's API is one its compiler serves.
	Charm *CharmParams
	Pump  *PumpParams
	Draw  *DrawParams

	// ReplaceEffect is api:ReplaceEffect's compiled parameter set
	// (replaceeffect_params.go), non-nil exactly when the API is
	// ReplaceEffect.
	ReplaceEffect *ReplaceEffectParams
	// Mana is api:Mana's compiled production parameters (mana_params.go),
	// non-nil exactly when the API is Mana; rules' mana half (Rules) is
	// derived from it.
	Mana *ManaParams
	// ManaReflected is api:ManaReflected's compiled parameter set
	// (manareflected_params.go), non-nil exactly when the API is
	// ManaReflected.
	ManaReflected *ManaReflectedParams
	// DealDamage is api:DealDamage's compiled parameter set
	// (dealdamage_params.go), non-nil exactly when the API is DealDamage.
	DealDamage *DealDamageParams
	// PutCounter is api:PutCounter's compiled parameter set
	// (putcounter_params.go), non-nil exactly when the API is PutCounter.
	PutCounter *PutCounterParams
	// Effect is api:Effect's compiled parameter set (effect_params.go),
	// non-nil exactly when the API is Effect.
	Effect *EffectParams
	// Targets is the generic targeting tier's compiled parameter set
	// (targets_params.go), non-nil for EVERY ability whatever its API
	// (Targets.Targeted() reports whether it targets).
	Targets *TargetParams
	// DelayedTrigger is api:DelayedTrigger's compiled parameter set
	// (delayedtrigger_params.go), non-nil exactly when the API is
	// DelayedTrigger.
	DelayedTrigger *DelayedTriggerParams
	// CopyPermanent is api:CopyPermanent's compiled parameter set
	// (copypermanent_params.go), non-nil exactly when the API is
	// CopyPermanent.
	CopyPermanent *CopyPermanentParams
	// Defined is the generic Defined-reference tier's compiled parameter set
	// (defined_params.go), non-nil for EVERY ability whatever its API.
	Defined *DefinedParams
	// Clone is api:Clone's compiled parameter set (clone_params.go),
	// non-nil exactly when the API is Clone.
	Clone *CloneParams
	// Dig is api:Dig's compiled parameter set (dig_params.go), non-nil
	// exactly when the API is Dig.
	Dig *DigParams
	// DigUntil is api:DigUntil's compiled parameter set
	// (diguntil_params.go), non-nil exactly when the API is DigUntil.
	DigUntil *DigUntilParams
	// RemoveCounter is api:RemoveCounter's compiled parameter set
	// (removecounter_params.go), non-nil exactly when the API is
	// RemoveCounter.
	RemoveCounter *RemoveCounterParams
	// Activation is the generic activation/condition tier's compiled
	// parameter set (activation_params.go), non-nil for EVERY ability
	// whatever its API.
	Activation *ActivationParams
	// Token is api:Token's compiled parameter set (token_params.go),
	// non-nil exactly when the API is Token.
	Token *TokenParams
	// Vote is api:Vote's compiled parameter set (vote_params.go),
	// non-nil exactly when the API is Vote.
	Vote *VoteParams
	// RepeatEach is api:RepeatEach's compiled parameter set (repeateach_params.go),
	// non-nil exactly when the API is RepeatEach.
	RepeatEach *RepeatEachParams
}

// NewSAFacts compiles sa's typed halves into a fresh record naming sa. The
// caller (rules' configured binding) adds its own half and publishes it.
func NewSAFacts(sa *cards.SA) *SAFacts {
	f := &SAFacts{SA: sa, Targets: compileTargets(sa), Defined: compileDefined(sa), Activation: compileActivation(sa)}
	if isChangeZoneSA(sa) {
		f.ChangeZone = compileChangeZone(sa, f.Targets, f.Defined)
	} else if isChangeZoneAllSA(sa) {
		f.ChangeZoneAll = compileChangeZoneAll(sa, f.Targets, f.Defined)
	} else if isAttachSA(sa) {
		f.Attach = compileAttach(sa, f.Targets)
	}
	compileTypedHalves(f, sa)
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
