package rules

import (
	"fmt"
	"math/rand/v2"

	"github.com/adams-shaun/gorge/state"
)

// rng is the engine's only source of randomness. Draws is counted so a replay
// can assert it consumed exactly the same number of values — the cheapest
// possible detector for "the engine changed underneath this log".
type rng struct {
	src    *rand.Rand
	pcg    *rand.PCG // kept so clone can copy the generator's exact position
	Draws  uint64
	seed   [2]uint64
	chance *chanceState // nil for ordinary games; owned by hypothetical clones
	// splice (the resolution kernel, lasagna spec §7.3): once Draws reaches
	// splice.at, continue on a copy of splice.next instead -- a hypothetical
	// world's re-execution of a recorded resolution prefix switches from
	// S0's generator to the world's own at the draw count the world was
	// forked at. Immutable, shared by clones; nil for every ordinary game.
	splice *rngSplice
}

// rngSplice is where a re-executed prefix switches generators.
type rngSplice struct {
	at   uint64
	next *rng
}

func (r *rng) forcePermutation(current, desired []state.ObjID) error {
	if r.chance == nil {
		return fmt.Errorf("planned shuffle requires a hypothetical engine")
	}
	if len(r.chance.prefix) > len(r.chance.draws) {
		return fmt.Errorf("planned shuffle cannot append before an unconsumed chance prefix")
	}
	if len(current) != len(desired) {
		return fmt.Errorf("planned shuffle is not a permutation")
	}
	// prefix supplies checked values by transcript position. Materialize the
	// already-generated suffix before extending it at the current position so
	// this Fisher-Yates pass consumes the requested values through IntN.
	r.chance.prefix = append(r.chance.prefix, r.chance.draws[len(r.chance.prefix):]...)
	work := append([]state.ObjID(nil), current...)
	seen := make(map[state.ObjID]bool, len(current))
	for _, id := range current {
		if id == 0 || seen[id] {
			return fmt.Errorf("planned shuffle source has duplicate object %d", id)
		}
		seen[id] = true
	}
	for _, id := range desired {
		if !seen[id] {
			return fmt.Errorf("planned shuffle is not a permutation")
		}
		delete(seen, id)
	}
	for i := len(work) - 1; i > 0; i-- {
		j := 0
		for work[j] != desired[i] {
			j++
		}
		r.chance.prefix = append(r.chance.prefix, ChanceDraw{Bound: i + 1, Value: j})
		work[i], work[j] = work[j], work[i]
	}
	r.Shuffle(current)
	return nil
}

func newRNG(seed uint64) *rng {
	s := [2]uint64{seed, seed ^ 0x9e3779b97f4a7c15}
	pcg := rand.NewPCG(s[0], s[1])
	return &rng{src: rand.New(pcg), pcg: pcg, seed: s}
}

// clone copies the generator at its exact position: the next IntN on the
// copy returns what the next IntN on the original would. PCG's binary
// marshalling is the stdlib's own round-trip for that state.
func (r *rng) clone() *rng {
	raw, err := r.pcg.MarshalBinary()
	if err != nil {
		panic("rules: PCG MarshalBinary: " + err.Error())
	}
	pcg := &rand.PCG{}
	if err := pcg.UnmarshalBinary(raw); err != nil {
		panic("rules: PCG UnmarshalBinary: " + err.Error())
	}
	return &rng{src: rand.New(pcg), pcg: pcg, Draws: r.Draws, seed: r.seed, chance: r.chance.clone(), splice: r.splice}
}

func (r *rng) IntN(n int) int {
	if r.splice != nil && r.Draws >= r.splice.at {
		nx := r.splice.next.clone()
		r.src, r.pcg, r.seed, r.chance, r.splice = nx.src, nx.pcg, nx.seed, nx.chance, nil
	}
	if r.chance != nil {
		r.chance.check(n)
	}
	r.Draws++
	v := r.src.IntN(n)
	if r.chance != nil {
		return r.chance.record(n, v)
	}
	return v
}

// Shuffle is Fisher-Yates and consumes exactly len(ids)-1 values, so draw count
// is a pure function of deck size.
func (r *rng) Shuffle(ids []state.ObjID) {
	for i := len(ids) - 1; i > 0; i-- {
		j := r.IntN(i + 1)
		ids[i], ids[j] = ids[j], ids[i]
	}
}

// RNGDraws exposes how many values the engine's own RNG has consumed. A
// replay (package replay) that reproduces the same event chain byte for
// byte but a different draw count would mean the engine's random walk
// changed even though every recorded choice still re-applied cleanly --
// this is the cheap complementary check alongside the hash chain that this
// type's own doc comment above already promises.
func (e *Engine) RNGDraws() uint64 { return e.rng.Draws }
