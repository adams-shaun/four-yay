package rules

import (
	"github.com/adams-shaun/gorge/rules/combat"
	"github.com/adams-shaun/gorge/state"
)

// The engine-level combat tests predate the rules/combat extraction (W5 step
// E5) and read the legality predicates as Engine methods. These test-only
// shims keep that spelling: each forwards to the combat package over
// asBoard(e), the same Board the production call sites pass, so a test still
// exercises exactly the code the declaration flow runs. They are not
// production Engine methods (codeshape's engineMethodCount counts non-test
// methods only).

type hiddenKeywordFlags = combat.HiddenKeywordFlags

func parseHiddenKeyword(k string) hiddenKeywordFlags { return combat.ParseHiddenKeyword(k) }

func landwalkSpec(k string) (string, bool) { return combat.LandwalkSpec(k) }

func restrictionPlayerTargetMatches(g *state.Game, spec string, defender, controller state.PlayerID, source state.ObjID, rememberedPlayers []state.PlayerID, attacked state.ObjID) bool {
	return combat.RestrictionTargetMatches(g, spec, defender, controller, source, rememberedPlayers, attacked)
}

func (e *Engine) canAttack(id state.ObjID) bool { return combat.CanAttack(asBoard(e), id) }

func (e *Engine) canAttackPair(id state.ObjID, defender state.PlayerID) bool {
	return combat.CanAttackPair(asBoard(e), id, defender)
}

func (e *Engine) attackableCreature(id state.ObjID) (*state.Object, bool) {
	return combat.AttackableCreature(asBoard(e), id)
}

func (e *Engine) attackRequirements(id state.ObjID) combat.RequirementSet {
	return combat.AttackRequirements(asBoard(e), id)
}

func (e *Engine) canBlock(blocker, attacker state.ObjID) bool {
	return combat.CanBlock(asBoard(e), blocker, attacker)
}

func (e *Engine) landwalkEvades(attacker state.ObjID) bool {
	return combat.LandwalkEvades(asBoard(e), attacker)
}

func (e *Engine) hasActiveGoad(o *state.Object) bool { return combat.HasActiveGoad(asBoard(e), o) }

func (e *Engine) goadedBy(o *state.Object, p state.PlayerID) bool {
	return combat.GoadedBy(asBoard(e), o, p)
}

func (e *Engine) maxAttackers() int { return combat.MaxAttackers(asBoard(e)) }

func (e *Engine) attackRestrictLimit(defender state.PlayerID) (int, bool) {
	return combat.AttackRestrictLimit(asBoard(e), defender, 0)
}

func (e *Engine) validateMinMaxBlockers(attacker state.ObjID, n int, defender state.PlayerID) error {
	return combat.ValidateMinMaxBlockers(asBoard(e), attacker, n, defender)
}

func (e *Engine) legalBlockerCount(attacker state.ObjID, defender state.PlayerID) int {
	return combat.LegalBlockerCount(asBoard(e), attacker, defender)
}

func (e *Engine) attackAllowedThroughDefender(id state.ObjID, defender state.PlayerID) bool {
	return combat.AttackAllowedThroughDefender(asBoard(e), id, defender)
}

func (e *Engine) playerAttackedYouTheirLastTurn(defender, you state.PlayerID) bool {
	return combat.PlayerAttackedYouTheirLastTurn(asBoard(e), defender, you)
}

func (e *Engine) derivedHiddenFlags(id state.ObjID) hiddenKeywordFlags {
	return combat.DerivedHiddenFlags(asBoard(e), id)
}

func (e *Engine) hasCantBlockKeyword(id state.ObjID) bool {
	return combat.HasCantBlockKeyword(asBoard(e), id)
}

func (e *Engine) hasCantAttackKeyword(id state.ObjID) bool {
	return combat.HasCantAttackKeyword(asBoard(e), id)
}

func (e *Engine) hasMustBeBlockedKeyword(id state.ObjID) bool {
	return combat.HasMustBeBlockedKeyword(asBoard(e), id)
}

func (e *Engine) blockRestricted(blocker, attacker state.ObjID) bool {
	return combat.BlockRestricted(asBoard(e), blocker, attacker)
}

func (e *Engine) minMaxBlockerBounds(attacker state.ObjID) (min, max int, minOK, maxOK, all bool) {
	return combat.MinMaxBlockerBounds(asBoard(e), attacker)
}

func (e *Engine) attackBlocked(id state.ObjID, defender state.PlayerID, attacked state.ObjID) bool {
	return combat.AttackBlocked(asBoard(e), id, defender, attacked)
}
