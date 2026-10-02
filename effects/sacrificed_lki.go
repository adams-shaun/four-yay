package effects

import "github.com/adams-shaun/gorge/state"

// SacrificedLKI is the last-known-information snapshot of an object about to
// be sacrificed (CR 608.2h: an effect that reads information from an object
// that is no longer in the zone it was expected in uses its last known
// information). Call it BEFORE the sacrifice's zone change.
//
// Power and toughness are the LAYER-DERIVED values the permanent has on the
// battlefield (Host.Power/Toughness), so an until-end-of-turn pump, prowess,
// an anthem, an Aura or Equipment and a base-P/T-setting effect are all part
// of "the sacrificed creature's power". state.SacrificedInfoOf alone reads the
// printed face plus +1/+1 counters -- the state package cannot see the layer
// system -- which made every Sacrificed$CardPower/CardToughness reader answer
// the printed number for a pumped creature. Every capture site goes through
// this one helper so they cannot drift.
//
// An object that is not a battlefield permanent keeps the base snapshot: it
// has no layer-derived P/T to read.
func SacrificedLKI(h Host, id state.ObjID) state.SacrificedInfo {
	g := h.Game()
	s := state.SacrificedInfoOf(g, id)
	if o := g.Obj(id); o != nil && o.Face() != nil && o.Zone == state.ZBattlefield {
		s.Power, s.Toughness = h.Power(id), h.Toughness(id)
	}
	return s
}
