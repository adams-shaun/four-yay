// attack_defender.go implements the CanAttackDefender permission static
// (CR 702.3b's effect half: "can attack as though it didn't have defender").
//
// The wall itself is canAttack's HasKeyword("Defender") check
// (rules/combat.go); this file is the READ that lifts it pair by pair:
//
//   - a face static (`S:Mode$ CanAttackDefender | ValidCard$ ...` — Felothar
//     the Steadfast's Creature.YouCtrl, Weathered Sentinels' Card.Self) is
//     enforced per (attacker, defender) pair through the activeStatics walk
//     with the shared gate grammar, the same shape attackPairCharge
//     (rules/attack_cost.go) uses for CantAttackUnless;
//   - an Effect-granted body (Assault Formation's AB$ Effect delivering
//     `SVar:CanAttack:Mode$ CanAttackDefender | ValidCard$
//     Creature.IsRemembered`) registers as a ContinuousEffect with
//     Restriction "CanAttackDefender" (effects/misc.go's effEffect case) and
//     is consulted through the same ContinuousEffect walk attackBlocked's
//     CantAttack loop takes, so the restrictionApplies machinery binds the
//     remembered target.
//
// Both routes are consulted at the ONE choke point canAttackPair puts around
// the Defender keyword; a creature without Defender never reaches this file.
//
// ValidAttacked$ (Weathered Sentinels'
// `Player.attackedYouTheirLastTurn`) is the defender-side gate: the static
// lifts the wall only against defenders who attacked the static's controller
// during THEIR last turn. The record of who attacked whom lives in the log
// (events.DeclareAttackers' Player IS the defender — rules/combat.go
// finishAttackers groups one event per defending player), so the read is a
// pure log scan with no state, rebuilt identically by replay: the same
// shape foretellCastAvailable (rules/altcast.go) takes for the same reason.
package rules

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// attackAllowedThroughDefender reports whether creature id, whose wall the
// Defender keyword would otherwise be, may still attack defender this combat
// because a CanAttackDefender static applies to the pair (CR 702.3b). A
// creature without Defender is never asked. Pure read, deterministic: the
// active()/activeStatics walks are the one shared scan every static consumer
// uses, and the ValidAttacked$ gate is a fixed-order log walk.
func (e *Engine) attackAllowedThroughDefender(id state.ObjID, defender state.PlayerID) bool {
	for ceI, ceL := 0, e.active(); ceI < len(ceL); ceI++ {
		ce := &ceL[ceI]
		if ce.Restriction != "CanAttackDefender" {
			continue
		}
		if !e.restrictionApplies(ce, id) {
			continue
		}
		// The source is deliberately NOT threaded here: the Player.CardOwner
		// resolution is scoped to CantAttack's Target$ walk (Xantcha). A
		// CardOwner qualifier in a ValidAttacked$ spec fails closed, exactly
		// as it did before that fix.
		if !e.attackedSpecHolds(ce.RestrictParams["ValidAttacked"], defender, ce.Controller, ce.RememberedPlayers) {
			continue
		}
		return true
	}
	for _, sv := range e.activeStatics("CanAttackDefender") {
		if !effects.CanAttackDefenderParamsReadable(sv.Params) {
			continue
		}
		if !e.continuousGateHolds(sv) {
			continue
		}
		spec := sv.ParamStr(cards.PKValidCard)
		if spec == "" {
			spec = sv.ParamStr(cards.PKValidCards)
		}
		if spec == "" || !e.matchesSpec(spec, id, e.specCtx(sv.Source, sv.Controller)) {
			continue
		}
		if !e.attackedSpecHolds(sv.Params["ValidAttacked"], defender, sv.Controller, nil) {
			continue
		}
		return true
	}
	return false
}

// attackedSpecHolds evaluates a CanAttackDefender line's ValidAttacked$ player
// spec against the defender under consideration (the static's "you" is its
// controller). An absent spec applies to every defender. The corpus spells
// exactly one scoping — Weathered Sentinels' `Player.attackedYouTheirLastTurn`
// — and it is evaluated here, not inside the generic player-spec matcher,
// because the record is log-derived and the matcher (effects.MatchesPlayerSpec)
// has no channel for it. Any OTHER part of a spec falls through to the shared
// restrictionPlayerSpecMatches grammar, so a plain `You`/`Opponent` scoping
// still resolves; a compound or unknown qualifier fails closed exactly as it
// would for CantAttack's Target$. No source is passed to that matcher, so a
// CardOwner qualifier — which only CantAttack's Target$ walk resolves — fails
// closed here as it always has.
func (e *Engine) attackedSpecHolds(spec string, defender, you state.PlayerID, rememberedPlayers []state.PlayerID) bool {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return true
	}
	for part := range strings.SplitSeq(spec, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if base := strings.TrimSuffix(part, ".attackedYouTheirLastTurn"); base != part {
			switch base {
			case "Player", "Any":
				// The base names the defender itself; the qualifier is the
				// whole read. Fail closed on any other base spelling.
				if e.playerAttackedYouTheirLastTurn(defender, you) {
					return true
				}
			}
			continue
		}
		if restrictionPlayerSpecMatches(e.G, part, defender, you, 0, rememberedPlayers) {
			return true
		}
	}
	return false
}

// playerAttackedYouTheirLastTurn reports whether defender attacked you during
// defender's last turn (CR 509-adjacent record keeping; Weathered Sentinels'
// oracle text: "Weathered Sentinels can attack players who attacked you
// during their last turn"). Derived from the log, never from mutable state:
//
//  1. the most recent TurnChange naming defender the incoming active player
//     (events.Apply's TurnChange case sets g.Active = e.Player, so the event
//     names the turn's owner);
//  2. within that turn's window — up to the next TurnChange or the log's
//     end — a DeclareAttackers event whose Player IS you (the defender of
//     that declaration, per finishAttackers' per-defender grouping) with a
//     non-empty attacker list.
//
// The query only ever runs while another seat is active (a defender candidate
// in the declare-attackers step is never the active player), so the window
// found is a completed turn and "last" is unambiguous. A player who never
// took a turn, or whose last turn never attacked you, fails.
func (e *Engine) playerAttackedYouTheirLastTurn(defender, you state.PlayerID) bool {
	if defender == you {
		return false
	}
	log := e.L.Events
	start := -1
	for i := len(log) - 1; i >= 0; i-- {
		if ev := log[i]; ev.Kind == events.TurnChange && ev.Player == defender {
			start = i
			break
		}
	}
	if start < 0 {
		return false
	}
	for i := start + 1; i < len(log); i++ {
		ev := log[i]
		if ev.Kind == events.TurnChange {
			break
		}
		if ev.Kind == events.DeclareAttackers && len(ev.IDs) > 0 && ev.Player == you {
			return true
		}
	}
	return false
}
