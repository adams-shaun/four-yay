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
// An object that is not a battlefield permanent has no layer-derived P/T to
// read. When it has ALREADY LEFT the battlefield -- the mana-ability path
// binds Ctx.Sacrificed after its Sac<...> cost was paid, so Lotus Blossom's
// own petal counters had been cleared by Move's fold -- the counters (and the
// counter part of P/T) are rebuilt from the log's departure record through the
// optional departureCountersHost read, so a sacrificed source still answers
// with the counters it left with (CR 608.2h).
func SacrificedLKI(h Host, id state.ObjID) state.SacrificedInfo {
	g := h.Game()
	s := state.SacrificedInfoOf(g, id)
	o := g.Obj(id)
	if o == nil || o.Face() == nil {
		return s
	}
	if o.Zone == state.ZBattlefield {
		s.Power, s.Toughness = h.Power(id), h.Toughness(id)
		return s
	}
	if dh, ok := h.(departureCountersHost); ok {
		if cs, ok := dh.DepartureCounters(id); ok {
			departed := state.Object{Counters: cs}
			dp, dt := departed.CounterPTTotals()
			s.Power = int32(o.Face().Power()) + dp
			s.Toughness = int32(o.Face().Toughness()) + dt
			s.Counters = positiveCounters(cs)
		}
	}
	return s
}

// sacrificedSourceLKI returns the snapshot this resolving spell or ability
// took of its OWN source when it sacrificed it -- a Sac<1/CARDNAME> cost
// (Ravenous Amulet's "Sacrifice this artifact: Each opponent loses life
// equal to the number of soul counters on this artifact", Shrine of Burning
// Rage, Golden Urn, Lotus Blossom, Time Bomb) or a resolution-time sacrifice
// of the source. The source-reading count heads (Count$CardCounters.<KIND>,
// CardPower, CardToughness) then read its last known information (CR
// 608.2h, 113.7a) instead of the card in the graveyard, whose counters
// Move's fold has already cleared and whose P/T is the bare printed face.
// A source that is back on the battlefield is read live: what is asked
// about then is the permanent that is there now.
func sacrificedSourceLKI(g *state.Game, c *Ctx) (*state.SacrificedInfo, bool) {
	if c == nil || c.Source == 0 || len(c.Sacrificed) == 0 {
		return nil, false
	}
	if o := g.Obj(c.Source); o != nil && o.Zone == state.ZBattlefield {
		return nil, false
	}
	for i := range c.Sacrificed {
		if c.Sacrificed[i].Obj == c.Source {
			return &c.Sacrificed[i], true
		}
	}
	return nil, false
}
