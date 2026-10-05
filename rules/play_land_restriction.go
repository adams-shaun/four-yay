package rules

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

type landRestrictionReader interface {
	Game() *state.Game
	activeStatics(string) []staticView
	playerSpecCtx(state.ObjID) effects.PlayerSpecCtx
	staticSpecCtx(staticView) effects.SpecContext
	matchesSpec(string, state.ObjID, effects.SpecContext) bool
	active() []ContinuousEffect
	restrictionApplies(*ContinuousEffect, state.ObjID) bool
}

// playLandForbidden is the common offer and commit-time rule for a land play.
func playLandForbidden(e landRestrictionReader, p state.PlayerID, from state.Zone, land state.ObjID) bool {
	for _, sv := range e.activeStatics("CantPlayLand") {
		playerScopeMatches := true
		for _, key := range [...]cards.ParamKey{cards.PKPlayer, cards.PKValidPlayer} {
			if spec := sv.ParamStr(key); spec != "" && !effects.MatchesPlayerSpecCtx(e.Game(), spec, p, sv.Controller, e.playerSpecCtx(sv.Source)) {
				playerScopeMatches = false
				break
			}
		}
		if !playerScopeMatches {
			continue
		}
		if origin, ok := sv.Param(cards.PKOrigin); ok {
			zones := effects.ZoneListOf(origin)
			if !zones.OK() || !zones.Admits(from) {
				continue
			}
		}
		spec := sv.ParamStr(cards.PKValidCard)
		if spec == "" {
			spec = sv.ParamStr(cards.PKValidCards)
		}
		if spec == "" || e.matchesSpec(spec, land, e.staticSpecCtx(sv)) {
			return true
		}
	}
	for _, ce := range e.active() {
		if cards.StaticModeOf(ce.Restriction) != cards.StaticCantPlayLand {
			continue
		}
		if origin, ok := ce.RestrictParamOk(cards.PKOrigin); ok {
			zones := effects.ZoneListOf(origin)
			if !zones.OK() || !zones.Admits(from) {
				continue
			}
		}
		spec := strings.TrimSpace(ce.RestrictParam(cards.PKPlayer))
		if spec == "" {
			spec = strings.TrimSpace(ce.RestrictParam(cards.PKValidPlayer))
		}
		if spec != "" && !effects.RestrictionPlayerSpecMatches(e.Game(), spec, p, ce.Controller, ce.Source, ce.RememberedPlayers) {
			continue
		}
		objSpec := strings.TrimSpace(ce.RestrictParam(cards.PKValidCard))
		if objSpec == "" {
			objSpec = strings.TrimSpace(ce.RestrictParam(cards.PKValidTarget))
		}
		if objSpec == "" {
			objSpec = strings.TrimSpace(ce.RestrictParam(cards.PKValidCards))
		}
		if objSpec == "" || e.restrictionApplies(&ce, land) {
			return true
		}
	}
	return false
}

func rejectLandPlay(forbidden func(state.PlayerID, state.Zone, state.ObjID) bool, pending **pendingCast, pc *pendingCast, emit func(events.Event) events.Event) bool {
	if !forbidden(pc.player, pc.from, pc.card) {
		return false
	}
	*pending = nil
	emit(events.Event{Kind: events.Note, Obj: pc.card, Text: "the play cannot play a restricted land"})
	return true
}
