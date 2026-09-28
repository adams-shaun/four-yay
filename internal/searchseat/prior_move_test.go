package searchseat

import (
	"math"
	"testing"
)

// preMoveCandidateScore is candidateScore exactly as it stood at 457efcb9
// (prior.go:155-185), kept here so the move to policynet is pinned bit for
// bit: the pn10 prior's ranking must not change.
func preMoveCandidateScore(kind string, scores []float32, choices []int) float64 {
	sp := func(z float64) float64 {
		if z > 0 {
			return z + math.Log1p(math.Exp(-z))
		}
		return math.Log1p(math.Exp(z))
	}
	if kind != "attackers" {
		if len(choices) != 1 || choices[0] < 0 || choices[0] >= len(scores) {
			return math.Inf(-1)
		}
		return float64(scores[choices[0]])
	}
	in := make([]bool, len(scores))
	for _, c := range choices {
		if c >= 0 && c < len(in) {
			in[c] = true
		}
	}
	ll := 0.0
	for i, s := range scores {
		if in[i] {
			ll -= sp(-float64(s))
		} else {
			ll -= sp(float64(s))
		}
	}
	return ll
}

func TestCandidateScoreMoveIsBitIdentical(t *testing.T) {
	scores := []float32{0.3, -2, 1.5, 0, 45, -45}
	for _, kind := range []string{"attackers", "cast", "blockers", "target"} {
		for _, choices := range [][]int{nil, {0}, {2}, {0, 2}, {4}, {5}, {0, 1, 2, 3, 4, 5}, {7}} {
			got := candidateScore(kind, scores, choices)
			want := preMoveCandidateScore(kind, scores, choices)
			if math.Float64bits(got) != math.Float64bits(want) {
				t.Errorf("candidateScore(%s, %v) = %v, pre-move %v", kind, choices, got, want)
			}
		}
	}
}
