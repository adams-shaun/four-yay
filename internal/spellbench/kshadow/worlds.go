package kshadow

import (
	"math/rand/v2"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/azmcts"
	"github.com/adams-shaun/gorge/internal/searchprobe"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/state"
)

// shadowWorlds is the honest world source over one staged shadow: world i
// is a hypothetical clone of the shadow with the opponent's hand and both
// libraries re-dealt from the same unseen pools (the redeal of spec D§5.2
// restricted to what the shadow itself dealt), future chance re-seeded.
// Public state, zone sizes and our own hand stay put.
type shadowWorlds struct {
	sh     *Shadow
	obs    *searchprobe.Collector
	seed   uint64
	k      int
	worlds []*rules.Engine
}

func (s *shadowWorlds) World(sim int) (azmcts.World, error) {
	var w *rules.Engine
	if s.k <= 0 {
		w = s.deal(sim)
	} else {
		i := sim % s.k
		for len(s.worlds) <= i {
			s.worlds = append(s.worlds, nil)
		}
		if s.worlds[i] == nil {
			s.worlds[i] = s.deal(i)
		}
		w = s.worlds[i].CloneHypothetical(mix(s.seed ^ mix(uint64(sim)+0x51)))
	}
	return azmcts.World{Engine: w, Observer: s.obs.Clone(), Hypothetical: true}, nil
}

// deal clones the shadow and re-deals its hidden zones.
func (s *shadowWorlds) deal(i int) *rules.Engine {
	ws := azmcts.RedealSeed(s.seed, i)
	w := s.sh.E.CloneHypothetical(ws[0])
	rng := rand.New(rand.NewPCG(ws[0], ws[1]))
	Redeal(w, s.sh, rng)
	return w
}

// Redeal re-deals the shadow's hidden zones on w (a clone of sh.E): the
// opponent's hand and library are one pool dealt uniformly into the same
// sizes, and both libraries are re-ordered. Every move is a secret event on
// w's own log.
func Redeal(w *rules.Engine, sh *Shadow, rng *rand.Rand) {
	g := w.G
	opp := 1 - sh.Me
	pool := append(append([]state.ObjID(nil), g.Zone(state.ZHand, opp)...), g.Zone(state.ZLibrary, opp)...)
	handN := len(g.Zone(state.ZHand, opp))
	rng.Shuffle(len(pool), func(i, j int) { pool[i], pool[j] = pool[j], pool[i] })
	inHand := map[state.ObjID]bool{} // lookup only
	for _, id := range pool[:handN] {
		inHand[id] = true
	}
	// Moves in pool order (deterministic).
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

func mix(x uint64) uint64 {
	x += 0x9e3779b97f4a7c15
	x = (x ^ (x >> 30)) * 0xbf58476d1ce4e5b9
	x = (x ^ (x >> 27)) * 0x94d049bb133111eb
	return x ^ (x >> 31)
}
