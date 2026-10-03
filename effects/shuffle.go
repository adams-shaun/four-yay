package effects

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func init() { Register("Shuffle", effShuffle) }

// effShuffle implements DB$ Shuffle (59 corpus files): shuffle one or more
// libraries. Defined$ names the libraries (You, Targeted,
// TargetedController, Remembered, ParentTarget, Player.IsRemembered,
// TriggeredTarget -- every player-valued Defined$ form the corpus uses); with
// no Defined$ and no ValidTgts$ the caster's own library is shuffled, which is
// what the bare "Shuffle your library" lines (a Cost$-only AB) mean.
//
// The shuffle itself is the same Fisher-Yates over h.Rand that a library
// search's own tail (effects/zone.go) and the engine's genesis deal both use,
// emitted as one Secret events.Shuffle per player in Defined-order: the exact
// order is hidden information (view redaction), the fact of the shuffle is
// not. A player target whose seat is invalid is skipped.
func effShuffle(h Host, c *Ctx, sa *cards.SA) {
	g := h.Game()
	var players []state.PlayerID
	if sa.ParamStr(cards.PKDefined) == "" && sa.ParamStr(cards.PKValidTgts) == "" {
		players = append(players, c.Controller)
	} else {
		for _, t := range Defined(h, c, sa) {
			if !t.IsPlayer {
				// A Defined$ that names objects (Defined$ Remembered on a card
				// sub-chain) still shuffles that object's OWNER's library --
				// "shuffle it into its owner's library" effects resolve the
				// library by the object's owner, never its controller.
				if o := g.Obj(t.Obj); o != nil {
					players = append(players, o.Owner)
				}
				continue
			}
			players = append(players, t.Player)
		}
	}
	for _, p := range players {
		if int(p) < 0 || int(p) >= len(g.Players) {
			continue
		}
		order := h.ShuffleLibrary(p, g.Zone(state.ZLibrary, p))
		h.Emit(events.Event{Kind: events.Shuffle, Player: p, IDs: order, Secret: true})
	}
}
