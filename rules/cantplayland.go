package rules

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/state"
)

// stat:CantPlayLand (CR 305.1's play permission) — a "you/players can't play
// lands" prohibition. BIG's Memory Vessel is the Standard carrier
// (`Player$ Player | Origin$ Hand`); 18 more exist corpus-wide, in four
// shapes that share the same grammar: a Player$ scope (You, Player, Opponent,
// a named/remembered player, a withMoreLandsThanYou qualifier), an Origin$
// zone scope (Hand, Graveyard, absent = any), a ValidCard$ card scope, and an
// IsPresent$ battlefield-count gate.
//
// The prohibition is applied at ONE choke point: filterCantPlayLand prunes
// the walk's assembled play_land options (rules/legal.go), so every land-play
// source -- the hand walk, the may-play-from-zone grants (mayPlayLandIds) and
// the bare-Mayhem graveyard play (mayhemLandPlayIds) -- is covered by
// construction rather than at each of the append sites. The option's object
// carries its own current zone, so an Origin$ scope is exact without the
// filter needing to know which walk offered it.
//
// cantPlayLand takes the narrow cantPlayLandEngine interface rather than
// *Engine so it is not counted by the engineSurface/engineMethodCount
// shrink-only ratchets (it is a free function, not an Engine method).

// cantPlayLandEngine is the slice of Engine the prohibition read needs.
type cantPlayLandEngine interface {
	Game() *state.Game
	activeStatics(mode string) []staticView
	staticGateHolds(sv staticView) bool
	matchesSpec(spec string, id state.ObjID, sc effects.SpecContext) bool
	staticSpecCtx(sv staticView) effects.SpecContext
}

// filterCantPlayLand removes every play_land option whose object an active
// Mode$ CantPlayLand static forbids its controller from playing, in place,
// reindexing the kept options (the same shape filterSplitSecondActions uses).
// It is a no-op with no CantPlayLand static active.
func filterCantPlayLand(e cantPlayLandEngine, p state.PlayerID, out []decision.Option) []decision.Option {
	g := e.Game()
	drop := false
	for i := range out {
		o := &out[i]
		if o.Kind != optPlayLand || o.Obj == 0 {
			continue
		}
		obj := g.Obj(o.Obj)
		if obj != nil && cantPlayLand(e, p, obj.Zone, o.Obj) {
			drop = true
			break
		}
	}
	if !drop {
		return out
	}
	kept := out[:0]
	for i := range out {
		o := out[i]
		if o.Kind == optPlayLand && o.Obj != 0 {
			if obj := g.Obj(o.Obj); obj != nil && cantPlayLand(e, p, obj.Zone, o.Obj) {
				continue
			}
		}
		o.Index = len(kept)
		kept = append(kept, o)
	}
	return kept
}

// cantPlayLand reports whether an active Mode$ CantPlayLand static forbids
// player p from playing land id from zone. A static whose IsPresent$/
// Condition$ gate does not hold, whose Player$ spec does not admit p, whose
// Origin$ does not name zone, or whose ValidCard$ does not select id is not a
// match. An unreadable Origin$ fails OPEN (does not prohibit), the same
// permissive direction a restriction's unreadable spec takes.
func cantPlayLand(e cantPlayLandEngine, p state.PlayerID, zone state.Zone, id state.ObjID) bool {
	for _, sv := range e.activeStatics("CantPlayLand") {
		if !e.staticGateHolds(sv) {
			continue
		}
		if ps := strings.TrimSpace(sv.ParamStr(cards.PKPlayer)); ps != "" {
			if !effects.MatchesPlayerSpecFrom(e.Game(), ps, p, sv.Controller, sv.Source) {
				continue
			}
		}
		if orig := strings.TrimSpace(sv.ParamStr(cards.PKOrigin)); orig != "" {
			zones, all, ok := effects.ParseZones(orig)
			if !ok {
				continue
			}
			if !all && !zoneIn(zone, zones) {
				continue
			}
		}
		if spec := strings.TrimSpace(sv.ParamStr(cards.PKValidCard)); spec != "" {
			if !e.matchesSpec(spec, id, e.staticSpecCtx(sv)) {
				continue
			}
		}
		return true
	}
	return false
}
