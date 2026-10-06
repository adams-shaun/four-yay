package rules

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/state"
)

// stat:CombatDamageNegatePower (Loot, the Anomaly: "If Loot's power is
// negative, he assigns combat damage as though his power were positive.") is
// read by combatDamageAmount, the ONE assignment-amount helper every combat
// damage site consumes, so the attacker's assignment, a blocker's hit-back,
// the division count and the as-unblocked gate all agree.
func init() { effects.RegisterNonAPI("stat:CombatDamageNegatePower") }

// combatDamageNegatePowerMatches reports whether a CombatDamageNegatePower
// static applies to candidate creature id. The match follows
// combatDamageToughnessMatches exactly -- the shared restriction/condition
// gates, the ClassLevel band, IsPresent$, and ValidCard$ resolved against the
// CANDIDATE with the static's host as the spec source (Loot's
// Card.Self+powerLT0 names Loot only while his power is negative) -- over the
// same assignmentStatics source walk.
func (e *Engine) combatDamageNegatePowerMatches(id state.ObjID) bool {
	for _, sv := range e.assignmentStatics("CombatDamageNegatePower") {
		if !e.restrictionGateHolds(sv, id) || !e.checkSVarHolds(sv) {
			continue
		}
		if !e.classBandGateHolds(sv.ParamStr(cards.PKClassBand), sv.Source) {
			continue
		}
		if spec := strings.TrimSpace(sv.ParamStr(cards.PKIsPresent)); spec != "" {
			if n, known := presentCountInZones(e, sv.ParamStr(cards.PKPresentZone), spec, sv.Source, sv.Controller); !known || n <= 0 {
				continue
			}
		}
		if !e.matchesSpec(sv.ParamStr(cards.PKValidCard), id, e.assignmentStaticSpecCtx(sv)) {
			continue
		}
		return true
	}
	return false
}
