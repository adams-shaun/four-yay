package effects

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

func init() {
	Register("TapOrUntapAll", effTapOrUntapAll)
}

// effTapOrUntapAll changes each matching battlefield permanent to the
// opposite tapped state. Player-kind ValidTgts$ scopes the sweep to the
// selected player's permanents, as in Turnabout.
func effTapOrUntapAll(h Host, c *Ctx, sa *cards.SA) {
	g := h.Game()
	spec := sa.ParamStr(cards.PKValidCards)
	if spec == "" {
		spec = "Permanent"
	}
	scope := targetPlayerKindScope(h, c, sa)
	defer beginActionBatch(h)()
	for _, p := range g.AliveFrom(0) {
		ids := append([]state.ObjID(nil), g.Zone(state.ZBattlefield, p)...)
		for _, id := range ids {
			if !inSweepScope(g, id, scope) || !MatchesSpecCtx(g, spec, id, c.SpecContext(c.Controller)) {
				continue
			}
			o := g.Obj(id)
			if o == nil || o.Zone != state.ZBattlefield {
				continue
			}
			if o.Tapped {
				TryUntap(h, id)
			} else {
				h.EmitTap(id, c.Controller, false)
			}
		}
	}
}
