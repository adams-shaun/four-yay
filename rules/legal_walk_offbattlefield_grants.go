package rules

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/rules/pay"
	"github.com/adams-shaun/gorge/state"
)

// grantedAnchorZoneOK is the shared zone constraint for both offering an
// SVar-anchored granted activation and accepting its priority answer.
func grantedAnchorZoneOK(o *state.Object, ab *cards.SA) bool {
	return o != nil && o.Face() != nil && ab != nil && abilityZoneOK(ab, o.Zone)
}

func isMaxSpeedCondition(condition string) bool { return condition == "MaxSpeed" }

func maxSpeedBoastHolds(w *legalWalk, id state.ObjID, ab *cards.SA, svar string) bool {
	return !strings.EqualFold(strings.TrimSpace(ab.ParamStr(cards.PKBoast)), "True") ||
		w.e.boastGateOK(id, -1, svar)
}

// offBattlefieldGrantedWalk offers the supported MaxSpeed AddAbility carriers
// collected from non-battlefield EffectZone$ memberships. Battlefield carriers
// stay with the existing layer/max-speed offer loops, avoiding duplicates.
func offBattlefieldGrantedWalk(w *legalWalk) {
	e, p := w.e, w.p
	out := &w.out
	for _, carrier := range w.actionStatics.addAbilityContinuous() {
		if carrier.Controller != p || !isMaxSpeedCondition(carrier.ParamStr(cards.PKCondition)) ||
			e.G.Players[carrier.Controller].Speed < maxSpeed {
			continue
		}
		o := e.G.Obj(carrier.Source)
		if o == nil || o.Zone == state.ZBattlefield || o.Face() == nil ||
			!e.matchesSpec(carrier.ParamStr(cards.PKAffected), carrier.Source,
				e.specCtx(carrier.Source, carrier.Controller)) {
			continue
		}
		sv := strings.TrimSpace(carrier.ParamStr(cards.PKAddAbility))
		ab := e.grantedSAFrom(carrier.Source, carrier.Source, sv)
		if !grantedAnchorZoneOK(o, ab) || cards.IsManaAbilityAPI(ab.API) {
			continue
		}
		if w.abilityRestricted(p, carrier.Source, ab) || e.castSuppressed(p, carrier.Source) {
			continue
		}
		cost := e.parseCost(ab.ParamStr(cards.PKCost))
		if pay.ActivationTapCostUnavailable(o, &cost) ||
			pay.TapCostSick(asPayer(e), carrier.Source, &cost, activatesAsIfHaste(e, carrier.Source)) ||
			!w.offerCastable(p, carrier.Source, cost, abilityScope(ab), true) ||
			!e.abilityTargetsAvailable(p, carrier.Source, ab) ||
			!e.abilityPresentHolds(p, carrier.Source, ab) || !e.adaptGateOK(carrier.Source, ab) {
			continue
		}
		if !maxSpeedBoastHolds(w, carrier.Source, ab, sv) {
			continue
		}
		*out = append(*out, decision.Option{Index: len(*out), Kind: "granted",
			Label: o.Face().Name + ": " + ab.ParamStr(cards.PKSpellDescription),
			Obj:   carrier.Source, SVar: sv})
	}
}
