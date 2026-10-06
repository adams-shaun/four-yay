package events

import "github.com/adams-shaun/gorge/state"

// foldCastBattlefield folds Kind CastBattlefield: it freezes the named
// battlefield permanents, as they stand in g right now, onto the spell's
// stack object (state.Object.CastBattlefield). The copy is taken at fold time
// from the game the event is applied to, so live play and log replay freeze
// identical facts; the event itself carries only the permanent IDs and their
// layer-derived P/T (which only rules can compute). A spell no longer on the
// stack is left alone.
func foldCastBattlefield(g *state.Game, e *Event) {
	o := g.Obj(e.Obj)
	if o == nil || o.Zone != state.ZStack {
		return
	}
	pt := make([]state.FrozenPT, 0, len(e.Pairs))
	for i, p := range e.Pairs {
		if i >= len(e.IDs) {
			break
		}
		pt = append(pt, state.FrozenPT{ID: e.IDs[i], Power: int32(uint32(p[0])), Toughness: int32(uint32(p[1]))})
	}
	o.CastBattlefield = state.FreezeBattlefield(g, e.IDs, pt)
}
