package effects

import (
	"strings"

	"github.com/adams-shaun/gorge/state"
)

// wordPredicateNewWords classifies the game-context predicate words this
// build reads that wordPredicate's own function body cannot host (that
// function sits 2 lines under its long-function ceiling):
//
//   - Forge's dealtDamagetoAny / dealtCombatDamagetoAny (bare words keyed in
//     the shared wordPredicateWordCodes table -- the ONE home of the
//     spellings, exactly like every other bare word there): the candidate
//     has dealt damage (combat damage) to anything this game.
//   - Forge's Card.attacking <PlayerSpec> rider: the candidate is attacking
//     the seat <PlayerSpec> names. The argument is validated against
//     playerSpecBaseCodes, the table that already owns the player-spec base
//     words, and later resolved by the player grammar's ONE home
//     (MatchesPlayerSpecCtx in attackingPlayerMatches) -- never a parallel
//     read. An argument that is not a player-spec base stays wordUnknown
//     and loud, so the census keeps reporting it (Seifer's `attacking Valid
//     Planeswalker.OppCtrl`, the referent spellings ChosenPlayer /
//     RememberedPlayer / EnchantedPlayer / TriggeredAttackedTarget).
//
// ok is false when p is none of the above, so wordPredicate falls through to
// its own tables unchanged.
func wordPredicateNewWords(p string) (kind wordKind, key string, ok bool) {
	switch wordPredicateWordCodes.Code(p) {
	case wordPredicateWordDealtDamageToAny:
		return wordDealtDamageToAny, "", true
	case wordPredicateWordDealtCombatDamageToAny:
		return wordDealtCombatDamageToAny, "", true
	}
	if rest, has := strings.CutPrefix(p, "attacking "); has {
		if arg := strings.TrimSpace(rest); playerSpecBaseCodes.Code(arg) != 0 {
			return wordAttackingPlayer, arg, true
		}
		return wordUnknown, rest, true
	}
	return wordUnknown, "", false
}

// attackingPlayerMatches evaluates wordAttackingPlayer: the candidate is
// attacking the seat the player-spec base <key> names, You = the evaluating
// controller (sc.You), resolved through MatchesPlayerSpecCtx -- the ONE home
// of the player grammar. Oviya, Automech Artisan's "each creature that's
// attacking one of your opponents has trample" INCLUDES your own attacking
// creature: the rider reads the DEFENDER's seat (state.Object.Attacking, CR
// 508.1), not the attacker's controller, so it is NOT the
// `attacking+Opponent` plus-spelling, which is "attacking creature
// CONTROLLED by an opponent". A candidate that is not attacking matches
// nothing, and an out-of-range seat (never a well-formed fold, but a
// hand-built state) fails closed rather than answering an invented player
// spec.
func attackingPlayerMatches(g *state.Game, key string, o *state.Object, sc SpecContext) bool {
	return o.IsAttacking && int(o.Attacking) < len(g.Players) &&
		MatchesPlayerSpecCtx(g, key, o.Attacking, sc.You, PlayerSpecCtx{})
}
