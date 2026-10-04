package rules

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// asUnblockedStaticMatches reports whether any battlefield
// AssignCombatDamageAsUnblocked static applies to candidate creature id, and
// whether the matching static is MANDATORY (no Optional$, the auto-accept
// reading) rather than the election-bearing Optional$ True every printed
// corpus carrier spells. The match follows castRestrictedUsing's pattern:
// ValidCard$ is resolved against the CANDIDATE (the attacking creature) with
// the static's host as the spec SOURCE (Indomitable Might's Aura resolves
// Creature.EnchantedBy against its own bearer), and the shared
// restriction/condition gates run first so an unmodelled condition fails
// closed rather than applying blanket. IsPresent$ gates on top through the
// shared countPresent walk (Siege Behemoth's "Card.Self+attacking" — both
// predicates are known), the same clause shape the trigger-side
// presentCondition reader evaluates.
func (e *Engine) asUnblockedStaticMatches(id state.ObjID) (matched, mandatory bool) {
	for _, sv := range e.assignmentStatics("AssignCombatDamageAsUnblocked") {
		if !e.restrictionGateHolds(sv, id) || !e.checkSVarHolds(sv) {
			continue
		}
		if !e.classBandGateHolds(sv.ParamStr(cards.PKClassBand), sv.Source) {
			continue
		}
		if spec := strings.TrimSpace(sv.ParamStr(cards.PKIsPresent)); spec != "" {
			if e.countPresent(spec, sv.Source, sv.Controller) <= 0 {
				continue
			}
		}
		if !e.matchesSpec(sv.ParamStr(cards.PKValidCard), id, e.staticSpecCtx(sv)) {
			continue
		}
		matched = true
		if strings.TrimSpace(sv.ParamStr(cards.PKOptional)) != "True" {
			return true, true
		}
	}
	return matched, false
}

// assignmentStatics collects every S:Mode$ <mode> line from ANY zone a
// static's EffectZone$ admits, for the combat damage-ASSIGNMENT static
// family (CombatDamageToughness, AssignCombatDamageAsUnblocked). These two
// modes are not battlefield-bound the way a lord's Continuous static is:
// Weight Advantage is a Conspiracy that functions from the COMMAND ZONE
// (EffectZone$ Command), so a battlefield-only walk can never see it and its
// controller's creatures would keep assigning power. The walk mirrors
// collectActionStatics/collectCostStatics exactly -- staticSourceZones in
// one fixed order, each static gated by effectZoneOK, the shared stack
// walked once under the first alive seat -- so the class of
// assignment-source statements is covered by construction and the next
// sibling mode added to this family cannot silently miss a non-battlefield
// source the way CombatDamageToughness did.
func (e *Engine) assignmentStatics(mode string) []staticView {
	var out []staticView
	for pi, p := range e.G.AliveFrom(0) {
		for _, z := range staticSourceZones {
			// The stack is a SHARED zone (state.Game.Zone returns g.Stack
			// for every player), so walking it under every alive seat would
			// collect each stack card's statics once per seat. Walk it once,
			// under the first alive seat, keeping staticSourceZones' order.
			if z == state.ZStack && pi > 0 {
				continue
			}
			for _, id := range e.G.Zone(z, p) {
				o := e.G.Obj(id)
				if o == nil || o.Face() == nil {
					continue
				}
				if e.printedAbilitiesGone(o) {
					// CR 708.8: a face-down permanent's printed statics do not
					// exist while it is face down.
					continue
				}
				for si, sn := 0, o.PileStaticCount(); si < sn; si++ {
					pst, ok := o.PileStaticAt(si)
					if !ok {
						continue
					}
					st := pst.Static
					if st.Mode != mode {
						continue
					}
					// The mode's own EffectZone$ gate. The default (Battlefield)
					// keeps a plain printed static exactly where activeStatics put
					// it; a static naming Command (Weight Advantage) is admitted
					// only while its source really sits there, which is the same
					// fail-closed direction effectZoneOK takes everywhere.
					if !effectZoneOK(st.ParamStr(cards.PKEffectZone), o.Zone) {
						continue
					}
					out = append(out, staticView{Source: id, Controller: o.Controller, Params: st.Params, PS: st.ParamSetOf(), SVars: pst.Face.SVars})
				}
			}
		}
	}
	// Effect-created assignment statics are admitted only for the exact mode
	// the registration branch records; they do not participate in the printed
	// EffectZone source-zone walk above. active() supplies lifetime and movement
	// filtering, and its order is stable, so append in registry order.
	for ceI, ceL := 0, e.active(); ceI < len(ceL); ceI++ {
		ce := &ceL[ceI]
		if ce.AssignmentStaticMode != mode {
			continue
		}
		out = append(out, staticView{
			Source: ce.Source, Controller: ce.Controller,
			Params: ce.AssignmentStaticParams, SVars: ce.AssignmentStaticSVars,
			Remembered: ce.Remembered,
		})
	}
	return out
}

// combatDamageToughnessMatches reports whether any
// CombatDamageToughness static applies to candidate creature id (CR 510.1:
// "assigns combat damage equal to its toughness rather than its power"). The
// match follows asUnblockedStaticMatches' pattern exactly -- the shared
// restriction/condition gates, the ClassLevel band, IsPresent$ through the
// shared countPresent walk, and ValidCard$ resolved against the CANDIDATE
// with the static's host as the spec SOURCE, so an Aura's
// Creature.EnchantedBy and a lord's Creature.YouCtrl both resolve. The source
// walk is assignmentStatics (every EffectZone$ the mode admits), so a
// command-zone Conspiracy (Weight Advantage) applies too.
func (e *Engine) combatDamageToughnessMatches(id state.ObjID) bool {
	for _, sv := range e.assignmentStatics("CombatDamageToughness") {
		if !e.restrictionGateHolds(sv, id) || !e.checkSVarHolds(sv) {
			continue
		}
		if !e.classBandGateHolds(sv.ParamStr(cards.PKClassBand), sv.Source) {
			continue
		}
		if spec := strings.TrimSpace(sv.ParamStr(cards.PKIsPresent)); spec != "" {
			if e.countPresent(spec, sv.Source, sv.Controller) <= 0 {
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

// combatDamageAmount is the amount creature id assigns as combat damage in a
// damage step: its TOUGHNESS when a CombatDamageToughness static applies (CR
// 510.1), else its power. This is the ONE read of the assignment source, so
// every consumer -- the attacker's own assignment, each blocker's hit-back,
// the division option count and the as-unblocked election gate -- cannot
// disagree. It reads through the layer walk (Toughness/Power are
// derivedScalar), so a static P/T bonus the same turn changes the amount
// exactly as it changes the characteristic.
//
// A creature whose effective amount is at most zero assigns no combat damage
// (an assignment of zero is not a decision and deals nothing), matching the
// power gate it replaces; callers that need the amount also read its sign.
//
// A NEGATIVE power assigns as though it were positive -- its absolute value
// -- while a CombatDamageNegatePower static applies (Loot, the Anomaly).
func (e *Engine) combatDamageAmount(id state.ObjID) int32 {
	if e.combatDamageToughnessMatches(id) {
		return e.Toughness(id)
	}
	p := e.Power(id)
	if p < 0 && e.combatDamageNegatePowerMatches(id) {
		return -p
	}
	return p
}

// tapPowerValueStatics collects every printed TapPowerValue static on the
// battlefield (the mode's carriers are all ordinary permanents), honouring
// each static's own EffectZone$ through activeStatics exactly as the
// continuous walk does. The assignment family's broader zone walk
// (assignmentStatics) is not needed: no corpus TapPowerValue source
// functions from outside the battlefield, and admitting one would be a new
// ticket, not this read.
func (e *Engine) tapPowerValueStatics() []staticView {
	return e.activeStatics("TapPowerValue")
}

// tapPowerSAScopeMatches reports whether a TapPowerValue static's ValidSA$
// admits the activated-action kind saKind ("Station", "Crew", "Saddle").
// Forge spells the value as a comma-separated OR list of
// "Activated.<kind>[+<qualifier>]" ("Activated.Station",
// "Activated.Crew+Vehicle,Activated.Saddle+Mount"); the first
// '+'-separated token after the "Activated." prefix is the action kind. An
// empty ValidSA$ admits every activated action; any other shape (a Spell
// scope, a kind this build cannot classify) fails closed, so a static the
// engine cannot scope never silently replaces a value.
func tapPowerSAScopeMatches(validSA, saKind string) bool {
	v := strings.TrimSpace(validSA)
	if v == "" {
		return true
	}
	for alt := range strings.SplitSeq(v, ",") {
		alt = strings.TrimSpace(alt)
		kind, rest, ok := strings.Cut(alt, ".")
		if !ok || kind != "Activated" {
			continue
		}
		token, _, _ := strings.Cut(rest, "+")
		if strings.TrimSpace(token) == saKind {
			return true
		}
	}
	return false
}

// tapPowerValue returns the value a creature id contributes when it is
// tapped to pay an activated action's tap-power amount -- CR 702.150a's
// Station ("charge counters equal to its power"), Crew's "total power N or
// greater" and Saddle's count. It honours any stat:TapPowerValue static
// whose ValidSA$ scopes that action kind and whose ValidCard$ matches the
// candidate: Tapestry Warden makes the controller's toughness>power
// creatures station using their TOUGHNESS, and the Pilot family makes its
// bearer act as though its power were N greater (Value$ <N>). The default is
// the creature's layer-derived power.
//
// This is the ONE read of that value, so the Station offer label, the
// affordability sum and the effect amount cannot disagree (the repo rule
// that a read rule has one home). It goes through the shared
// restriction/condition/ClassLevel gates, so an unmodelled gate fails closed
// to plain power rather than replacing the value blanket.
func (e *Engine) tapPowerValue(id state.ObjID, saKind string) int32 {
	for _, sv := range e.tapPowerValueStatics() {
		if !e.restrictionGateHolds(sv, id) || !e.checkSVarHolds(sv) {
			continue
		}
		if !e.classBandGateHolds(sv.ParamStr(cards.PKClassBand), sv.Source) {
			continue
		}
		if !tapPowerSAScopeMatches(sv.ParamStr(cards.PKValidSA), saKind) {
			continue
		}
		if !e.matchesSpec(sv.ParamStr(cards.PKValidCard), id, e.staticSpecCtx(sv)) {
			continue
		}
		v := strings.TrimSpace(sv.ParamStr(cards.PKValue))
		if v == "Toughness" {
			return e.Toughness(id)
		}
		// A numeric Value$ is Forge's additive "as though its power were
		// N greater" (every numeric corpus carrier's Description says
		// exactly that), not a replacement with the literal number.
		if n, err := strconv.Atoi(v); err == nil {
			return e.Power(id) + int32(n)
		}
	}
	return e.Power(id)
}

// tapCostSAKind names the activated-action kind an ability's tap-power cost
// belongs to, for the TapPowerValue ValidSA$ scope. The keyword expansions
// stamp the minted tap-cost ability with `Keyword$ <kw>` (cards/kw_crew.go
// sets Keyword$ Crew; the Saddle expansion the same way), so the keyword is
// the action kind a static like Giant Ox's `ValidSA$ Activated.Crew+Vehicle`
// scopes to. A hand-written tapXType ability (Mossbridge Troll) and every
// non-tap-cost ability read as "", which only an empty ValidSA$ admits. The
// read is the SA's own keyword, so an ability can never be scoped by another
// action's static.
func tapCostSAKind(ab *cards.SA) string {
	if ab == nil {
		return ""
	}
	return strings.TrimSpace(ab.ParamStr(cards.PKKeyword))
}
