package policynet

import "math"

// CandidateScore is a trained policy head's opinion of one candidate answer
// to a decision, given the per-option scores Model.Score returned for it
// (scores parallels the decision's Options; choices are the candidate's
// option indices).
//
// subset selects the head's loss family. Attackers and blockers are trained
// with a per-option binary (BCE) loss, so a declared subset's log-likelihood
// is sum log sigmoid(s_i) over the included options plus sum
// log(1 - sigmoid(s_j)) over the excluded ones (out-of-range choices are
// ignored). Priority and target are softmax (CE) heads: a single option's
// score ranks it, and any other shape -- no choice, several, an index out of
// range -- is -Inf.
//
// It is the one definition shared by the pn10 candidate prior
// (internal/searchseat delegates to it), the AlphaZero search's priors
// (internal/azmcts) and ticket 3's visit-distribution loss, which spreads a
// subset target onto the same Bernoulli form. candidateSoftplus is kept apart
// from the training softplus in net.go on purpose: the pn10 prior ranked with
// this exact function before it moved here, and the training version's +/-30
// cut-offs would change low bits of a log-likelihood and could reorder
// near-tied candidates.
func CandidateScore(subset bool, scores []float32, choices []int) float64 {
	if !subset {
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
			ll -= candidateSoftplus(-float64(s)) // log sigmoid(s)
		} else {
			ll -= candidateSoftplus(float64(s)) // log(1 - sigmoid(s))
		}
	}
	return ll
}

// candidateSoftplus is log(1 + e^z) without overflow -- byte for byte the
// softplus internal/searchseat's prior used before CandidateScore moved here.
func candidateSoftplus(z float64) float64 {
	if z > 0 {
		return z + math.Log1p(math.Exp(-z))
	}
	return math.Log1p(math.Exp(z))
}
