package effects

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// targetPlayerKindScope resolves a player-kind ValidTgts$ (Player/Opponent) on
// an *All sweep to the seats that sweep is restricted to, through the same
// referent the TargetedPlayerCtrl filter predicate uses so the two cannot
// drift.  A non-nil scope that resolves to no player leaves an EMPTY map: the
// caller sees a non-nil scope and fails CLOSED (it sweeps nothing), matching
// the Defined/TargetedPlayerCtrl direction rather than falling back to every
// seat.
//
// A non-player ValidTgts$ (e.g. Creature) and an absent one both return nil,
// so the filter-only sweep stands.  It is the ONE implementation shared by
// every *All primitive that carries the "each <object> target player controls"
// shape (DamageAll's Aggravate/Simoon/Chandra, Bold Pyromancer; AnimateAll's
// Curious Colossus), so a new carrier cannot silently sweep every seat because
// only one of them grew the branch.
func targetPlayerKindScope(h Host, c *Ctx, sa *cards.SA) map[state.PlayerID]bool {
	tg := TargetsOf(sa).ValidTgts
	if tg == "" || !playerSpecBaseKnown(tg) {
		return nil
	}
	sc := c.SpecContext(c.Controller)
	sc.ResolutionTargets = targetedGroup(c)
	players, _ := controlReferentPlayers(h.Game(), sc, "ControlledBy", "TargetedPlayer")
	scope := make(map[state.PlayerID]bool, len(players))
	for _, p := range players {
		scope[p] = true
	}
	return scope
}

// inSweepScope reports whether an object at id is inside a target-player-kind
// scope.  A nil scope admits every object; a non-nil one admits only objects
// whose controller is in the set (an object that has left the game is
// excluded).
func inSweepScope(g *state.Game, id state.ObjID, scope map[state.PlayerID]bool) bool {
	if scope == nil {
		return true
	}
	o := g.Obj(id)
	return o != nil && scope[o.Controller]
}
