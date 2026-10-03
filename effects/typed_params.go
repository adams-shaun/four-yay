package effects

import (
	"github.com/adams-shaun/gorge/cards"
)

// This file is the machinery the Charm/Pump/Draw typed parameter compilers
// share (W4 step 3 of
// docs/superpowers/specs/2026-10-03-rules-engine-lasagna-design.md, section
// 8): the identity rule a compiled struct answers by, and the configured
// binding's compile step. The compile-time unread check and its Note are the
// shared unreadKeys/noteUnreadParams (changezoneall_params.go). Each
// compiler (charm_params.go, pump_params.go, draw_params.go) is the ONLY
// reader of its API's own parameters; its XxxOf accessor returns the
// configured record from SAFacts, else a front-cache entry, else a fresh
// compile -- ChangeZoneOf's shape (changezone_params.go).

// paramBinding is the Params map a typed struct was compiled from and the
// map's size then: the struct answers only for that exact map instance
// (cards.SameParamMap), so a cards.ResolveSVar copy sharing its template's
// parse reads the template's struct while a copy whose Params were rewritten
// recompiles.
type paramBinding struct {
	src map[string]string
	n   int
}

func bindParams(sa *cards.SA) paramBinding {
	return paramBinding{src: sa.Params, n: len(sa.Params)}
}

func (b *paramBinding) boundTo(m map[string]string) bool {
	return b.n == len(m) && cards.SameParamMap(b.src, m)
}

// compileTypedHalves fills f's per-API typed parameter structs (Charm, Pump,
// Draw, ReplaceEffect, Mana, ManaReflected, DealDamage, PutCounter, Effect,
// DelayedTrigger, CopyPermanent, Clone, Dig, DigUntil, RemoveCounter, Token, Vote, RepeatEach) for the APIs their compilers serve
// (NewSAFacts' second half).
func compileTypedHalves(f *SAFacts, sa *cards.SA) {
	if isModalSA(sa) {
		f.Charm = compileCharm(sa, f.Defined)
	}
	if sa.API == "Pump" {
		f.Pump = compilePump(sa, f.Defined)
	}
	if sa.API == "Draw" {
		f.Draw = compileDraw(sa)
	}
	if sa.API == "ReplaceEffect" {
		f.ReplaceEffect = compileReplaceEffect(sa)
	}
	if sa.API == "Mana" {
		f.Mana = compileMana(sa, f.Defined)
	}
	if sa.API == "ManaReflected" {
		f.ManaReflected = compileManaReflected(sa, f.Defined, f.Activation)
	}
	if isDealDamageSA(sa) {
		f.DealDamage = compileDealDamage(sa)
	}
	if isPutCounterSA(sa) {
		f.PutCounter = compilePutCounter(sa)
	}
	if isEffectSA(sa) {
		f.Effect = compileEffect(sa)
	}
	if isDelayedTriggerSA(sa) {
		f.DelayedTrigger = compileDelayedTrigger(sa, f.Activation)
	}
	if isCopyPermanentSA(sa) {
		f.CopyPermanent = compileCopyPermanent(sa, f.Defined)
	}
	if isCloneSA(sa) {
		f.Clone = compileClone(sa, f.Defined)
	}
	if isDigSA(sa) {
		f.Dig = compileDig(sa)
	}
	if isDigUntilSA(sa) {
		f.DigUntil = compileDigUntil(sa, f.Defined)
	}
	if isRemoveCounterSA(sa) {
		f.RemoveCounter = compileRemoveCounter(sa, f.Defined)
	}
	if isTokenSA(sa) {
		f.Token = compileToken(sa)
	}
	if isVoteSA(sa) {
		f.Vote = compileVote(sa)
	}
	if isRepeatEachSA(sa) {
		f.RepeatEach = compileRepeatEach(sa, f.Defined)
	}
}
