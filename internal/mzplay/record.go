package mzplay

import (
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/mzbridge"
	"github.com/adams-shaun/gorge/state"
)

// Row is one training record of one seat: what ComputerPlayerMCTS2
// .calculateActions hands StateEncoder.addLabeledState, plus the value label
// the game's end fills in.
type Row struct {
	// IDs is the root state's feature ids for the recording seat.
	IDs []int32
	// Policy is the root children's visit counts by action index.
	Policy []mzbridge.PolicyEntry
	// Score is the root's mean value (stateScore) on upstream's scale,
	// [-1, 1] from the recording seat's side.
	Score float64
	Type  mzbridge.ActionType
	// Value is the training target (resultLabel), set by LabelValues.
	Value float64
}

// NoWinner is the winner of a game nobody won: a drawn game, or one stopped
// at the turn cap.
const NoWinner = -1

// SeatWon is the flag upstream labels a seat's records with
// (ParallelDataGenerator.java:379-380): seat A (0) with playerA.hasWon(),
// seat B (1) with its NEGATION. So when nobody won, A's records are labelled
// as a loss and B's as a win. Kept, because it is what the reference run
// trained on.
func SeatWon(seat, winner int) bool {
	aWon := winner == 0
	if seat == 0 {
		return aWon
	}
	return !aWon
}

// LabelValues is ParallelDataGenerator.generateLabeledStatesForGame: from
// the last record back, y = lambda*y_next + (1-lambda)*score, starting at
// +1 for a won game and -1 otherwise.
func LabelValues(rows []Row, won bool, lambda float64) {
	future := -1.0
	if won {
		future = 1.0
	}
	for i := len(rows) - 1; i >= 0; i-- {
		future = lambda*future + rows[i].Score*(1-lambda)
		rows[i].Value = future
	}
}

// ScoreFromRootValue maps azmcts's root mean value, the searching seat's
// win probability in [0,1], onto upstream's stateScore in [-1,1].
func ScoreFromRootValue(v float64) float64 { return 2*v - 1 }

// policyFromVisits is ComputerPlayerMCTS2.getActionVec: raw visit counts by
// action index, children sharing an index added together.
func policyFromVisits(index, visits []int) []mzbridge.PolicyEntry {
	var out []mzbridge.PolicyEntry
	for i, idx := range index {
		if i >= len(visits) || visits[i] <= 0 {
			continue
		}
		merged := false
		for j := range out {
			if out[j].Index == idx {
				out[j].Visits += float32(visits[i])
				merged = true
				break
			}
		}
		if !merged {
			out = append(out, mzbridge.PolicyEntry{Index: idx, Visits: float32(visits[i])})
		}
	}
	return out
}

// Combat. Upstream never searches a whole declaration: selectAttackers asks
// one yes/no per available attacker ("attack with: X?", a CHOOSE_USE record
// each) and selectBlockers asks each available blocker which attacker it
// blocks (a CHOOSE_TARGET record each, "Stop Choosing" for none). gorge's
// search is over whole declarations, so one root is projected onto those
// per-creature records as MARGINALS: a creature's count for an answer is the
// summed visits of the root candidates that give that answer for it. Every
// record of one declaration therefore shares one root, its counts sum to the
// root's visits, and the records differ from upstream's in being marginals
// of one joint search rather than successive conditional searches.

// AttackerMarginal is one potential attacker's yes/no record.
type AttackerMarginal struct {
	Obj state.ObjID
	// Yes and No are the summed visits of the root candidates in which the
	// creature attacks and does not attack.
	Yes, No int
	// Chosen is the creature's answer in the declaration played.
	Chosen bool
}

// optionAt maps Option.Index values to positions in d.Options.
func optionAt(d *decision.Decision, index int) (decision.Option, bool) {
	if index >= 0 && index < len(d.Options) && d.Options[index].Index == index {
		return d.Options[index], true
	}
	for _, o := range d.Options {
		if o.Index == index {
			return o, true
		}
	}
	return decision.Option{}, false
}

// AttackMarginals projects an attackers root: cands and visits are the root
// candidates (azmcts.Result.Candidates and Visits) and choice the candidate
// played. One entry per distinct attacking creature, in option order.
func AttackMarginals(d *decision.Decision, cands []decision.Intent, visits []int, choice int) []AttackerMarginal {
	var out []AttackerMarginal
	at := func(obj state.ObjID) int {
		for i := range out {
			if out[i].Obj == obj {
				return i
			}
		}
		return -1
	}
	for _, o := range d.Options {
		if o.Obj != 0 && at(o.Obj) < 0 {
			out = append(out, AttackerMarginal{Obj: o.Obj})
		}
	}
	attacks := make([]bool, len(out))
	for ci, c := range cands {
		for i := range attacks {
			attacks[i] = false
		}
		for _, idx := range c.Choices {
			if o, ok := optionAt(d, idx); ok {
				if i := at(o.Obj); i >= 0 {
					attacks[i] = true
				}
			}
		}
		v := 0
		if ci < len(visits) {
			v = visits[ci]
		}
		for i := range out {
			if attacks[i] {
				out[i].Yes += v
			} else {
				out[i].No += v
			}
			if ci == choice {
				out[i].Chosen = attacks[i]
			}
		}
	}
	return out
}

// BlockerMarginal is one potential blocker's which-attacker record.
type BlockerMarginal struct {
	Obj state.ObjID
	// Attackers are the attackers the creature may block, in option order;
	// Visits parallels it: the summed visits of the root candidates in which
	// the creature blocks that attacker.
	Attackers []state.ObjID
	Visits    []int
	// Stop is the summed visits of the candidates in which it does not block.
	Stop int
	// Chosen is the attacker it blocks in the declaration played, 0 for none.
	Chosen state.ObjID
}

// BlockMarginals projects a blockers root, one entry per distinct blocking
// creature in option order. A creature that blocks several attackers in one
// candidate (a multi-block ability) counts that candidate for each.
func BlockMarginals(d *decision.Decision, cands []decision.Intent, visits []int, choice int) []BlockerMarginal {
	var out []BlockerMarginal
	at := func(obj state.ObjID) int {
		for i := range out {
			if out[i].Obj == obj {
				return i
			}
		}
		return -1
	}
	for _, o := range d.Options {
		if o.Obj == 0 || o.Attacker == 0 {
			continue
		}
		i := at(o.Obj)
		if i < 0 {
			out = append(out, BlockerMarginal{Obj: o.Obj})
			i = len(out) - 1
		}
		known := false
		for _, a := range out[i].Attackers {
			known = known || a == o.Attacker
		}
		if !known {
			out[i].Attackers = append(out[i].Attackers, o.Attacker)
			out[i].Visits = append(out[i].Visits, 0)
		}
	}
	blocks := make([]bool, len(out))
	for ci, c := range cands {
		for i := range blocks {
			blocks[i] = false
		}
		v := 0
		if ci < len(visits) {
			v = visits[ci]
		}
		for _, idx := range c.Choices {
			o, ok := optionAt(d, idx)
			if !ok {
				continue
			}
			i := at(o.Obj)
			if i < 0 {
				continue
			}
			for k, a := range out[i].Attackers {
				if a == o.Attacker {
					out[i].Visits[k] += v
					if ci == choice && !blocks[i] {
						out[i].Chosen = a
					}
				}
			}
			blocks[i] = true
		}
		for i := range out {
			if !blocks[i] {
				out[i].Stop += v
			}
		}
	}
	return out
}
