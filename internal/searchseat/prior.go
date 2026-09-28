package searchseat

import (
	"sort"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/policynet"
	"github.com/adams-shaun/gorge/internal/searchprobe"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/seat"
	"github.com/adams-shaun/gorge/view"
)

// The policy prior (pn10): a trained policynet checkpoint chooses which
// candidates the teacher spends its rollouts on. Without it the attackers and
// cast arms roll out a FIXED, hand-ordered list capped at Limit (bot answer,
// then no-attack / all-in / toggles; or pass, then the pool in JSON order).
// With it they enumerate wider (PriorWiden), the non-bot candidates are ranked
// by the head's opinion of them on the actor's own view, and the top PriorTopK
// are kept behind the bot's answer. Candidate 0 is always the bot's answer
// (the label contract, BotIndex == 0).
//
// The enumerators are capped as they go, so today's Limit-capped list is a
// PREFIX of the widened enumeration. That is what makes the fallback exact:
// whenever the prior declines to rank (a cast decision outside the head's
// trained distribution, or a candidate that cannot be mapped back to option
// indices), truncating the widened list to Limit IS today's list.

// enumLimit is the candidate enumeration cap of the attackers and cast arms:
// Limit without a Prior (today's list), the widened cap with one.
func (o Options) enumLimit() int {
	if o.Prior == nil {
		return o.Limit
	}
	return o.priorWiden()
}

// priorWiden is the enumeration cap (bot answer included) with a Prior.
func (o Options) priorWiden() int {
	if o.PriorWiden > 0 {
		return o.PriorWiden
	}
	return max(o.Limit, 16)
}

// priorTopK is the number of non-bot candidates a Prior keeps.
func (o Options) priorTopK() int {
	if o.PriorTopK > 0 {
		return o.PriorTopK
	}
	return o.Limit - 1
}

// PriorBudget reports the EFFECTIVE prior budget -- PriorTopK and PriorWiden
// with their zero-value defaults applied -- for a caller that logs the
// budget it ran (cmd/searchteacher's summary header). It ignores whether a
// Prior is set.
func (o Options) PriorBudget() (topK, widen int) {
	return o.priorTopK(), o.priorWiden()
}

// applyPrior re-ranks and prunes the attackers / cast candidate list when a
// Prior is set, recording the diagnostics on tr. Every other case returns
// cands unchanged: a nil Prior (candidates already enumerated with Limit),
// and the blockers arm (never widened).
func applyPrior(collector *searchprobe.Collector, e *rules.Engine, d *decision.Decision, bot decision.Intent, kind string, cands [][]searchprobe.Action, opts Options, tr *Trace) [][]searchprobe.Action {
	if opts.Prior == nil || (kind != "attackers" && kind != "cast") {
		return cands
	}
	tr.PriorEnumerated = len(cands)
	unguided := func() [][]searchprobe.Action {
		if len(cands) > opts.Limit {
			cands = cands[:opts.Limit]
		}
		tr.PriorKept = len(cands)
		return cands
	}
	if len(cands) < 2 {
		return unguided()
	}
	// The cast arm is re-ranked only on the shape the priority head was
	// trained on (pn01's gate, the one seat.PolicyNetBot applies): residual
	// prior active, >= 2 castable objects, the bot's answer one cast / ability
	// / pass option. Outside it the head's opinion is untrained, so today's
	// list stands.
	if kind == "cast" && !seat.PriorityInDistribution(d, bot, opts.Prior.ResidualW) {
		return unguided()
	}
	choices := make([][]int, len(cands))
	for i, c := range cands {
		in, err := collector.Match(d, c)
		if err != nil {
			return unguided()
		}
		choices[i] = in.Choices
	}
	v := view.Project(e.G, e, d.Player, d)
	keep, changed := priorOrder(opts.Prior, v, d, bot, kind, choices, opts.priorTopK())
	out := make([][]searchprobe.Action, len(keep))
	for i, k := range keep {
		out[i] = cands[k]
	}
	tr.PriorKept, tr.PriorRanked, tr.PriorChanged = len(out), true, changed
	return out
}

// priorOrder scores the decision once on the actor's view v -- the residual
// prior's BotPick marked on the bot answer's options, the contract a residual
// checkpoint trained under -- and returns the candidate indices to keep: 0
// (the bot's answer) first, then the topK best non-bot candidates by
// candidateScore, DESCENDING, ties in enumeration order (a stable sort over
// indices; no map is ranged). changed reports whether the kept set differs
// from the same-budget unguided list, the first len(keep) candidates.
//
// choices[i] is candidate i's option indices in d (choices[0] the bot's).
func priorOrder(m *policynet.Model, v view.View, d *decision.Decision, bot decision.Intent, kind string, choices [][]int, topK int) (keep []int, changed bool) {
	st := policynet.EncodeStateWith(m.Features, v, d.Player, nil)
	enc := make([]policynet.Option, len(d.Options))
	for i := range d.Options {
		enc[i] = policynet.EncodeOptionWith(m.Features, v, d.Player, d.Kind, d.Options[i], i, len(d.Options))
	}
	seat.MarkBotPicks(d, enc, bot)
	scores := m.Score(st, enc)

	rest := make([]int, 0, len(choices)-1)
	val := make([]float64, len(choices))
	for i := 1; i < len(choices); i++ {
		rest = append(rest, i)
		val[i] = candidateScore(kind, scores, choices[i])
	}
	sort.SliceStable(rest, func(a, b int) bool { return val[rest[a]] > val[rest[b]] })
	if topK < 0 {
		topK = 0
	}
	if len(rest) > topK {
		rest = rest[:topK]
	}
	keep = append([]int{0}, rest...)
	// Same-budget unguided list: indices 0..len(keep)-1. The kept set differs
	// from it exactly when some kept index lies beyond it.
	for _, k := range keep {
		if k >= len(keep) {
			changed = true
		}
	}
	return keep, changed
}

// candidateScore is the head's opinion of one candidate: the attackers head
// is a per-option BCE head (subset log-likelihood), every other kind this
// prior ranks is a single-option softmax head. The definition lives in
// policynet.CandidateScore, shared with the AlphaZero search; the pn10 prior
// has never ranked blockers, so kind == "attackers" is exactly the pre-move
// behaviour (pinned by TestCandidateScoreMoveIsBitIdentical).
func candidateScore(kind string, scores []float32, choices []int) float64 {
	return policynet.CandidateScore(kind == "attackers", scores, choices)
}
