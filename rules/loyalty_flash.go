package rules

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/state"
)

// loyaltyAtInstantSpeed reports whether a live loyalty-timing grant lets p
// activate the loyalty abilities of permanent id "on any player's turn any
// time you could cast an instant" -- i.e. whenever p holds priority, not only
// at the CR 606.3 sorcery timing. It is the Mode$ CastWithFlash static whose
// ValidSA$ names Activated.Loyalty, from either delivery route:
//
//   - a printed S: line (The Wandering Emperor's Card.Self+ThisTurnEntered,
//     Teferi, Master of Time), read through activeStatics with the same
//     Caster$ / timing / EffectZone$ / ValidCard$ gates castWithFlashTargets
//     applies to a spell;
//   - an Effect-delivered registration (Jace's Machinations' InstantJace,
//     Teferi, Temporal Archmage's emblem), which effects' effEffect registers
//     as a CastWithFlash restriction only when LoyaltyFlashParamsReadable
//     admits it (Caster$ You, ValidSA$ Activated.Loyalty).
//
// The grant changes timing only: the caller keeps CR 606.3's
// once-per-permanent-per-turn limit.
func (e *Engine) loyaltyAtInstantSpeed(p state.PlayerID, id state.ObjID) bool {
	for _, sv := range e.activeStatics("CastWithFlash") {
		if !loyaltyFlashValidSA(sv.ParamStr(cards.PKValidSA)) {
			continue
		}
		if !e.actorMatches(sv, "Caster", p) || !e.staticTimingGate(sv) {
			continue
		}
		if az, ok := sv.Param(cards.PKEffectZone); ok {
			src := e.G.Obj(sv.Source)
			if src == nil || !affectedZoneOK(az, src.Zone) {
				continue
			}
		}
		if e.matchesSpec(sv.ParamStr(cards.PKValidCard), id, e.staticSpecCtx(sv)) {
			return true
		}
	}
	for ceI, ceL := 0, e.active(); ceI < len(ceL); ceI++ {
		ce := &ceL[ceI]
		if ce.Restriction != "CastWithFlash" || ce.Controller != p ||
			!effects.LoyaltyFlashParamsReadable(ce.RestrictParams) {
			continue
		}
		sc := e.specCtx(ce.Source, ce.Controller)
		for _, r := range ce.Remembered {
			sc.Remembered = append(sc.Remembered, state.Target{Obj: r})
		}
		if e.matchesSpec(ce.RestrictParams["ValidCard"], id, sc) {
			return true
		}
	}
	return false
}

// loyaltyFlashValidSA reports whether a CastWithFlash ValidSA$ names the
// loyalty-ability alternative (`Activated.Loyalty`).
func loyaltyFlashValidSA(v string) bool {
	for alt := range effects.FilterAlternatives(v) {
		if strings.TrimSpace(alt) == "Activated.Loyalty" {
			return true
		}
	}
	return false
}
