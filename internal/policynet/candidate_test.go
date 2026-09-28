package policynet

import (
	"math"
	"testing"
)

func TestCandidateScoreSingleChoice(t *testing.T) {
	scores := []float32{0.5, -1.25, 2}
	if got := CandidateScore(false, scores, []int{2}); got != 2 {
		t.Fatalf("single choice 2 = %v, want its score 2", got)
	}
	for _, bad := range [][]int{nil, {0, 1}, {-1}, {3}} {
		if got := CandidateScore(false, scores, bad); !math.IsInf(got, -1) {
			t.Errorf("single-option head, choices %v = %v, want -Inf", bad, got)
		}
	}
}

func TestCandidateScoreSubsetIsBernoulliLogLikelihood(t *testing.T) {
	scores := []float32{0.5, -1.25, 2}
	sig := func(z float64) float64 { return 1 / (1 + math.Exp(-z)) }
	want := math.Log(sig(0.5)) + math.Log(1-sig(-1.25)) + math.Log(sig(2))
	got := CandidateScore(true, scores, []int{0, 2})
	if math.Abs(got-want) > 1e-12 {
		t.Fatalf("subset {0,2} = %v, want %v", got, want)
	}
	wantNone := math.Log(1-sig(0.5)) + math.Log(1-sig(-1.25)) + math.Log(1-sig(2))
	if got := CandidateScore(true, scores, nil); math.Abs(got-wantNone) > 1e-12 {
		t.Fatalf("empty declaration = %v, want %v", got, wantNone)
	}
	if again := CandidateScore(true, scores, []int{0, 2, 9, -1}); again != got {
		t.Fatalf("out-of-range choices changed the score: %v vs %v", again, got)
	}
}
