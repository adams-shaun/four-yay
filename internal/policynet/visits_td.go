package policynet

import "sort"

// VisitTDTargets is upstream MageZero's value label over the records of ONE
// visit corpus file: within one seat's trajectory through one game (GameID,
// Seed, Seat), ordered by Sequence,
//
//	y_i = lambda*y_{i+1} + (1-lambda)*h_i
//
// where h_i is the record's root value and the y after the trajectory's last
// record is the game outcome. lambda 1 is the plain outcome. ok[i] is false
// for a record whose outcome is unknown (a truncated or halted game): it has
// no target. Game ids repeat between botbench runs, so the caller passes one
// file's records at a time. The map is a lookup; trajectories are walked in
// first-seen order.
func VisitTDTargets(recs []VisitRecord, lambda float64) (y []float64, ok []bool) {
	type key struct {
		game string
		seed uint64
		seat int
	}
	lookup := map[key]int{}
	var trajs [][]int
	for i := range recs {
		k := key{recs[i].GameID, recs[i].Seed, recs[i].Seat}
		t, seen := lookup[k]
		if !seen {
			t = len(trajs)
			lookup[k] = t
			trajs = append(trajs, nil)
		}
		trajs[t] = append(trajs[t], i)
	}
	y, ok = make([]float64, len(recs)), make([]bool, len(recs))
	for _, tr := range trajs {
		sort.SliceStable(tr, func(a, b int) bool { return recs[tr[a]].Sequence < recs[tr[b]].Sequence })
		last := recs[tr[len(tr)-1]]
		if !last.OutcomeKnown {
			continue
		}
		next := last.Outcome
		for j := len(tr) - 1; j >= 0; j-- {
			i := tr[j]
			next = lambda*next + (1-lambda)*recs[i].RootValue
			y[i], ok[i] = next, recs[i].OutcomeKnown
		}
	}
	return y, ok
}

// visitPolicy applies ScalePolicy to a visit example's policy term and its
// score gradients.
func (lc LossConfig) visitPolicy(parts LossParts, dys []float64) (LossParts, []float64) {
	if !lc.ScalePolicy {
		return parts, dys
	}
	parts.Rank *= lc.PolicyWeight
	parts.Total *= lc.PolicyWeight
	scaleGrads(dys, lc.PolicyWeight)
	return parts, dys
}
