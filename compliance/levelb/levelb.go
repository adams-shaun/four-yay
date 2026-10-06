// Package levelb turns a card's IR into its list of level-B requirements
// (spec docs/superpowers/specs/2026-10-05-compliance-level-b.md section 1).
//
// A requirement is one thing a level-B scenario must exercise: an activated
// ability, a trigger, a static, or an attack/block. The classifier is a pure
// function of the IR -- it runs no game and imports neither rules nor
// compliance/oraclegen -- so the requirement set, and therefore the level-B
// outstanding count, does not move when a template lands; only verdicts do.
package levelb

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
)

// Requirement is one level-B obligation a card carries.
type Requirement struct {
	// Key is "<family>#<face>.<slot>": activate#0.2, trigger#0.1,
	// static#1.0, combat#0.attack.
	Key string
	// Family is one of "activate", "trigger", "static", "combat".
	Family string
	// Face is the index into Card.Faces.
	Face int
	// Slot is the index into the owning Face slice (Abilities for activate,
	// Triggers for trigger, Statics for static) or "attack"/"block" for
	// combat.
	Slot string
	// Sub is the level-B sub-family that names the v1 template serving the
	// requirement, or the ".face:<n>" gap for a face after 0.
	Sub string
	// CoveredByA is true only for a face-0, non-land trigger that the
	// level-A cast-resolve scenario already settles.
	CoveredByA bool
	// Gap is non-empty exactly when no v1 template serves the requirement.
	Gap string
}

// combatKeywords are the keywords whose presence on a creature face gives it
// level-B attack/block requirements (spec section 1).
var combatKeywords = []string{
	"Flying", "Reach", "First Strike", "Double Strike", "Deathtouch",
	"Trample", "Lifelink", "Menace", "Vigilance", "Indestructible",
	"Defender", "Skulk", "Fear", "Intimidate", "Shadow", "Horsemanship",
	"Flanking", "Bushido", "Afflict", "Annihilator", "Battle cry",
	"Melee", "Rampage", "Provoke", "Toxic", "Infect", "Wither",
}

// servableStaticModes are static modes a v1 template serves directly (they
// need no "offered" observation): DisableTriggers is observed through the
// stack and CombatDamageToughness through combat damage. Every other combat
// mode stays static.combat (a legality gap). The table is the single home for
// the mapping so a second mode of the same shape is one row.
var servableStaticModes = []struct{ mode, sub string }{
	{"DisableTriggers", "static.disable-triggers"},
	{"CombatDamageToughness", "static.combat-damage-toughness"},
}

// combatStaticModes are the combat-legality statics whose presence on a face
// gives it level-B attack/block requirements (spec section 1, section 2.3).
var combatStaticModes = []string{
	"CantAttack", "CantBlock", "CantBlockBy", "MustAttack", "MustBlock",
	"MinMaxBlocker", "CanAttackDefender", "CombatDamageToughness",
	"CombatDamageNegatePower",
}

// Requirements returns the level-B requirements for every face of c, in face
// order and then IR order, with combat last. It returns nil for a basic land,
// mirroring isBasicLand at compliance/gate/gate.go:248.
func Requirements(c *cards.Card) []Requirement {
	if c == nil || len(c.Faces) == 0 || isBasicLand(c) {
		return nil
	}
	var out []Requirement
	// Non-combat requirements: face order, then IR order within a face.
	for fi, f := range c.Faces {
		for ai, sa := range f.Abilities {
			if !sa.IsActivated() {
				continue
			}
			sub, gap := classifyActivate(sa)
			out = append(out, newReq("activate", fi, strconv.Itoa(ai), sub, gap, false))
		}
		for ti := range f.Triggers {
			sub, gap, covered := classifyTrigger(f, &f.Triggers[ti])
			out = append(out, newReq("trigger", fi, strconv.Itoa(ti), sub, gap, covered))
		}
		for si := range f.Statics {
			sub, gap := classifyStatic(&f.Statics[si])
			out = append(out, newReq("static", fi, strconv.Itoa(si), sub, gap, false))
		}
	}
	// Combat requirements last, in face order.
	for fi, f := range c.Faces {
		if !faceHasCombat(f) {
			continue
		}
		out = append(out,
			newReq("combat", fi, "attack", "combat.attack", "", false),
			newReq("combat", fi, "block", "combat.block", "", false),
		)
	}
	return out
}

// newReq builds a Requirement, applying the face-after-0 gap: v1 templates
// serve Faces[0] only, as level A does, so every face > 0 requirement is a
// gap.
func newReq(family string, face int, slot, sub, gap string, covered bool) Requirement {
	r := Requirement{
		Key:        family + "#" + strconv.Itoa(face) + "." + slot,
		Family:     family,
		Face:       face,
		Slot:       slot,
		Sub:        sub,
		CoveredByA: covered,
		Gap:        gap,
	}
	if face > 0 {
		r.Sub = sub + ".face:" + strconv.Itoa(face)
		r.Gap = "face " + strconv.Itoa(face)
		r.CoveredByA = false
	}
	return r
}

// classifyActivate returns the activated-ability sub-family and its gap.
func classifyActivate(sa *cards.SA) (sub, gap string) {
	zone, _ := sa.Param(cards.PKActivationZone)
	if zone != "" && !strings.EqualFold(zone, "Battlefield") {
		return "activate.zone:" + zone, "activation zone " + zone
	}
	if strings.EqualFold(sa.API, "Mana") {
		return "activate.mana", ""
	}
	return "activate.battlefield", ""
}

// classifyTrigger returns the trigger's sub-family, its gap, and whether the
// level-A scenario already covers it.
func classifyTrigger(f *cards.Face, t *cards.Trigger) (sub, gap string, covered bool) {
	gapMode := func() (string, string, bool) {
		return "trigger.gap:" + t.Mode, "trigger mode " + t.Mode, false
	}
	switch t.ModeKind() {
	case cards.TriggerChangesZone:
		dest := t.ParamStr(cards.PKDestination)
		origin := t.ParamStr(cards.PKOrigin)
		self := namesSelf(t.ParamStr(cards.PKValidCard))
		if strings.EqualFold(dest, "Battlefield") {
			if !self {
				return "trigger.etb-other", "", false
			}
			// A self-ETB trigger is the one shape the level-A cast-resolve
			// scenario settles, but only off a non-land: a land's play-land
			// scenario never puts a spell on the stack.
			if f.IsLand() {
				return gapMode()
			}
			return "trigger.etb-other", "", true
		}
		if strings.EqualFold(origin, "Battlefield") && strings.EqualFold(dest, "Graveyard") && self {
			return "trigger.dies", "", false
		}
		return gapMode()

	case cards.TriggerAttacks:
		// "naming the card or its controller": the card itself, or a
		// creature its controller controls.
		if namesSelf(t.ParamStr(cards.PKValidCard)) || namesYouCtrl(t.ParamStr(cards.PKValidCard)) {
			return "trigger.attacks", "", false
		}
		return gapMode()

	case cards.TriggerAttackersDeclared:
		if strings.EqualFold(t.ParamStr(cards.PKAttackingPlayer), "You") || namesYouCtrl(t.ParamStr(cards.PKValidAttackers)) {
			return "trigger.attacks", "", false
		}
		return gapMode()

	case cards.TriggerDamageDone:
		if namesSelf(t.ParamStr(cards.PKValidSource)) && strings.EqualFold(t.ParamStr(cards.PKCombatDamage), "True") {
			return "trigger.combat-damage", "", false
		}
		return gapMode()

	case cards.TriggerSpellCast:
		if strings.EqualFold(t.ParamStr(cards.PKValidActivatingPlayer), "You") {
			return "trigger.spell-cast", "", false
		}
		return gapMode()

	case cards.TriggerBecomesTarget:
		if namesSelf(t.ParamStr(cards.PKValidTarget)) {
			return "trigger.becomes-target", "", false
		}
		return gapMode()

	case cards.TriggerLifeGained:
		if strings.EqualFold(t.ParamStr(cards.PKValidPlayer), "You") {
			return "trigger.life-gained", "", false
		}
		return gapMode()

	case cards.TriggerDrawn:
		if strings.EqualFold(t.ParamStr(cards.PKValidPlayer), "You") || namesYouCtrl(t.ParamStr(cards.PKValidCard)) {
			return "trigger.drawn", "", false
		}
		return gapMode()

	case cards.TriggerPhase:
		// The phase recipe scripts only p0's phases, so a ValidPlayer$
		// naming another player is a gap.
		if vp := t.ParamStr(cards.PKValidPlayer); vp != "" && !strings.EqualFold(vp, "You") {
			return gapMode()
		}
		switch t.ParamStr(cards.PKPhase) {
		case "End of Turn", "BeginCombat", "Upkeep", "Draw":
			return "trigger.phase", "", false
		}
		return gapMode()

	default:
		return gapMode()
	}
}

// classifyStatic returns the static's sub-family and its gap.
func classifyStatic(st *cards.Static) (sub, gap string) {
	switch st.ModeKind() {
	case cards.StaticContinuous:
		return "static.continuous", ""
	case cards.StaticReduceCost:
		return "static.cost", ""
	case cards.StaticRaiseCost:
		// v1 can only probe our own cast. Opponent-cast taxation needs a
		// p1 turn, which the level-B cast recipes do not yet provide.
		return "static.cost", "opponent-cast cost static"
	}
	for _, s := range servableStaticModes {
		if strings.EqualFold(st.Mode, s.mode) {
			return s.sub, ""
		}
	}
	if isCombatStaticMode(st.Mode) {
		return "static.combat", "legality static"
	}
	return "static.gap:" + st.Mode, "static mode " + st.Mode
}

// isCombatStaticMode reports whether mode names a combat-legality static.
func isCombatStaticMode(mode string) bool {
	for _, m := range combatStaticModes {
		if strings.EqualFold(mode, m) {
			return true
		}
	}
	return false
}

// faceHasCombat reports whether f is a creature face carrying a combat
// keyword or a combat-legality static.
func faceHasCombat(f *cards.Face) bool {
	if !f.IsCreature() {
		return false
	}
	for _, k := range combatKeywords {
		if f.HasKeyword(k) {
			return true
		}
	}
	for i := range f.Statics {
		if isCombatStaticMode(f.Statics[i].Mode) {
			return true
		}
	}
	return false
}

// isBasicLand mirrors compliance/gate/gate.go:248.
func isBasicLand(c *cards.Card) bool {
	if len(c.Faces) == 0 {
		return false
	}
	basic, land := false, false
	for _, t := range c.Faces[0].Types {
		basic = basic || strings.EqualFold(t, "Basic")
		land = land || strings.EqualFold(t, "Land")
	}
	return basic && land
}

// filterHasToken reports whether a card filter carries want as a token in any
// comma-separated alternative. A Forge filter is "<class>.<qualifier>[+<qualifier>]"
// per alternative, so a token is split on both "." and "+" (the qualifier
// separator): `Card.Self+kicked` carries tokens Card, Self and kicked.
func filterHasToken(filter, want string) bool {
	for _, alt := range strings.Split(filter, ",") {
		for _, tok := range strings.FieldsFunc(alt, func(r rune) bool { return r == '.' || r == '+' }) {
			if strings.EqualFold(strings.TrimSpace(tok), want) {
				return true
			}
		}
	}
	return false
}

// namesSelf reports whether a card filter selects the source card itself, e.g.
// `Card.Self` or `Card.Self+wasCastByYou`.
func namesSelf(filter string) bool { return filterHasToken(filter, "Self") }

// namesYouCtrl reports whether a card filter selects a permanent you control,
// e.g. `Creature.YouCtrl` or `Creature.Other+YouCtrl`.
func namesYouCtrl(filter string) bool { return filterHasToken(filter, "YouCtrl") }
