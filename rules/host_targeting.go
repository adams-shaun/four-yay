package rules

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// host_targeting.go is the engine's implementation of effects.HostTargeting's
// target offer (rules-engine refactor spec W1d), with the coarse ValidTgts$
// shape reads the offer and target legality share.

// LegalTargets satisfies effects.Host for target-changing effects. It exposes
// the same census used by cast and trigger target decisions, so a redirect
// cannot bypass protection, CantTarget, stack-kind, zone, or filter legality.
//
// The census runs with the RESOLVING stack object as both source and
// excludeSelf whenever one exists -- exactly the placement ask's own call
// (pushTrigger -> askTarget passes the stack object id): CR 115.5 withholds
// the ability on the stack from targeting itself, never its source permanent,
// so a trigger whose source is a legal target may target it (Kor Outfitter's
// Attach sub attaches to Kor Outfitter). A direct, off-stack resolution (no
// resolving object) keeps the caller's source.
func (e *Engine) LegalTargets(chooser state.PlayerID, source state.ObjID, sa *cards.SA) []state.Target {
	if e.resolvingObj != 0 {
		source = e.resolvingObj
	}
	cs := e.legalTargetCandidates(chooser, source, source, sa)
	out := make([]state.Target, 0, len(cs))
	for _, c := range cs {
		if c.kind == "player" {
			out = append(out, state.Target{Player: c.player, IsPlayer: true})
		} else {
			out = append(out, state.Target{Obj: c.obj})
		}
	}
	return out
}

// targetsPermanents reads the coarse shape of a ValidTgts spec (the player
// half is compiled once: effects.TgtValidPlayers, effects.SpecTargetsPlayers).
// The per-object predicate work is effects.MatchesSpec.
func targetsPermanents(spec string) bool {
	for _, t := range [...]string{"Creature", "Any", "Permanent", "Artifact",
		"Enchantment", "Land", "Planeswalker", "Card"} {
		if strings.Contains(spec, t) {
			return true
		}
	}
	return false
}
