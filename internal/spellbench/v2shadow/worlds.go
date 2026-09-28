package v2shadow

import (
	"math/rand/v2"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/state"
)

// Redeal re-deals the shadow's hidden zones on w (a hypothetical clone of
// sh.E): the opponent's hand and library are one pool dealt uniformly into
// the same sizes, and both libraries are re-ordered (the M1 redeal world,
// spec D§5.2, restricted to what the shadow itself dealt: it never reads
// the real game). Every move is a secret event on w's own log.
func Redeal(w *rules.Engine, sh *Shadow, rng *rand.Rand) {
	g := w.G
	opp := 1 - sh.Me
	pool := append(append([]state.ObjID(nil), g.Zone(state.ZHand, opp)...), g.Zone(state.ZLibrary, opp)...)
	handN := len(g.Zone(state.ZHand, opp))
	rng.Shuffle(len(pool), func(i, j int) { pool[i], pool[j] = pool[j], pool[i] })
	inHand := make(map[state.ObjID]bool, handN) // lookup only
	for _, id := range pool[:handN] {
		inHand[id] = true
	}
	for _, id := range pool {
		o := g.Obj(id)
		if o == nil {
			continue
		}
		switch {
		case inHand[id] && o.Zone != state.ZHand:
			events.Emit(g, w.L, events.Event{Kind: events.MoveZone, Obj: id, From: o.Zone, To: state.ZHand, Secret: true})
		case !inHand[id] && o.Zone != state.ZLibrary:
			events.Emit(g, w.L, events.Event{Kind: events.MoveZone, Obj: id, From: o.Zone, To: state.ZLibrary, Secret: true})
		}
	}
	for _, pl := range []state.PlayerID{sh.Me, opp} {
		lib := append([]state.ObjID(nil), g.Zone(state.ZLibrary, pl)...)
		rng.Shuffle(len(lib), func(i, j int) { lib[i], lib[j] = lib[j], lib[i] })
		events.Emit(g, w.L, events.Event{Kind: events.Shuffle, Player: pl, IDs: lib, Secret: true})
	}
}
