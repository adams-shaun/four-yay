package rules

import (
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// castBattlefieldEvent builds events.CastBattlefield for a spell whose script
// counts "the battlefield as you cast this spell"
// (Count$LastStateBattlefieldWithFallback), so the head reads the cast-time
// facts instead of the resolution-time battlefield.
//
// Timing (CR 601.2a): the freeze is taken once, the moment the spell reaches
// the stack and before it announces targets, divisions or costs. The head also
// sizes cast-time limits (TargetMax$, DividedAsYouChoose$), and those are
// announced after the spell is on the stack (CR 601.2b-d), so a resolution-
// only capture would be too late. A mana-window resume re-enters the cast flow
// with pc.pushed set and never re-freezes; a permanent a cost then sacrifices
// (CR 601.2h) therefore still counts, because "as you cast this spell" is read
// at announcement. The event deep-copies the battlefield at fold time and
// carries only the IDs plus each permanent's layer-derived P/T, which only the
// rules layer walk can supply.
//
// ok is false (nothing to emit) for any spell that does not read the head.
func castBattlefieldEvent(g *state.Game, derived derivedPTReader, spell state.ObjID, caster state.PlayerID) (events.Event, bool) {
	o := g.Obj(spell)
	if o == nil || o.Face() == nil || !effects.SVarsReadCastBattlefield(o.Face().SVars) {
		return events.Event{}, false
	}
	var ids []state.ObjID
	var pairs [][2]state.ObjID
	for p := range g.Players {
		for _, id := range g.Zone(state.ZBattlefield, state.PlayerID(p)) {
			ids = append(ids, id)
			pairs = append(pairs, [2]state.ObjID{state.ObjID(uint32(derived.Power(id))), state.ObjID(uint32(derived.Toughness(id)))})
		}
	}
	return events.Event{Kind: events.CastBattlefield, Obj: spell, Player: caster, IDs: ids, Pairs: pairs}, true
}

// derivedPTReader is the layer-derived P/T read castBattlefieldEvent needs
// (the Engine implements it).
type derivedPTReader interface {
	Power(state.ObjID) int32
	Toughness(state.ObjID) int32
}
