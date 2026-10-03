package botpolicy

import (
	"math/rand/v2"
	"testing"

	"github.com/adams-shaun/gorge/decision"
)

func throwAsk(aiRandom bool) *decision.Decision {
	d := &decision.Decision{Kind: decision.KModes, Min: 1, Max: 1, AIRandom: aiRandom}
	for i, l := range []string{"Rock", "Paper", "Scissors"} {
		d.Options = append(d.Options, decision.Option{Index: i, Kind: "mode", Label: l})
	}
	return d
}

// An AIRandom KModes ask (AILogic$ Random: Face to Face's throw) is drawn
// from the seat's seeded rng under every policy -- deterministic per seed,
// varied across seeds -- while an ordinary KModes ask keeps the fixed
// first-option answer.
func TestAIRandomModesAskDrawsFromTheSeatRng(t *testing.T) {
	for _, pol := range []struct {
		name   string
		decide func(Board, *decision.Decision, *rand.Rand) decision.Intent
	}{{"Decide", Decide}, {"ExploreDecide", ExploreDecide}, {"LegacyDecide", LegacyDecide}} {
		name, decide := pol.name, pol.decide
		seen := map[int]bool{}
		for seed := uint64(0); seed < 16; seed++ {
			a := decide(Board{}, throwAsk(true), rand.New(rand.NewPCG(seed, 1)))
			b := decide(Board{}, throwAsk(true), rand.New(rand.NewPCG(seed, 1)))
			if len(a.Choices) != 1 || len(b.Choices) != 1 || a.Choices[0] != b.Choices[0] {
				t.Fatalf("%s seed %d: answers %v / %v, want one identical pick", name, seed, a.Choices, b.Choices)
			}
			seen[a.Choices[0]] = true
			if in := decide(Board{}, throwAsk(false), rand.New(rand.NewPCG(seed, 1))); len(in.Choices) != 1 || in.Choices[0] != 0 {
				t.Fatalf("%s seed %d: ordinary KModes answered %v, want [0]", name, seed, in.Choices)
			}
		}
		if len(seen) < 2 {
			t.Fatalf("%s: sixteen seeds all threw %v", name, seen)
		}
	}
}
