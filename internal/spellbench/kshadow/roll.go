package kshadow

import (
	"context"
	"fmt"
	"math/rand/v2"
	"sort"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/searchprobe"
	"github.com/adams-shaun/gorge/internal/spellbench/builtins"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/seat"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// RollConfig is the rollout search's budget (ModeRoll): determinized flat
// Monte Carlo over the kernel's own candidates. Every candidate is played
// in the same W redealt worlds (common random numbers), both seats are
// rolled out by the rollout policy to a turn horizon, and the leaf is
// scored by searchprobe.LeafValue (1/0 at game over). The fallback's pick
// is always a candidate and is overridden only past Margin.
type RollConfig struct {
	Worlds   int     // W: dealt worlds per decision
	Horizon  int     // rollout turns beyond the current one
	MaxSteps int     // submit cap per rollout
	Margin   float64 // mean-value gain needed to override the fallback
	TopK     int     // priority candidates kept, by the fallback's scores
	Rollout  string  // "bot" (gorge bot, autopay) or "tactical" (sb-tactical)
	// Arbiter restricts the candidates to v1agent.Tactical's pick and
	// sb-tactical's (on the shadow): the search only arbitrates between
	// the two policies where they disagree.
	Arbiter bool
	// BaseTactical makes sb-tactical's pick the default the search must
	// beat (arbiter only), instead of v1agent.Tactical's.
	BaseTactical bool
}

// DefaultRoll is the starting budget.
func DefaultRoll() RollConfig {
	return RollConfig{Worlds: 12, Horizon: 2, MaxSteps: 600, Margin: 0.03, TopK: 5, Rollout: "bot"}
}

// action is one root candidate, resolvable on any world clone of the root
// (world clones share object ids and the pending decision).
type action struct {
	kind  string // pass, option, payment, attackers, blockers
	opt   int    // option index (option)
	obj   state.ObjID
	label string
	// Combat declarations: the option indices to choose.
	choices []int
}

// intentOn resolves the action at w's pending decision.
func (a *action) intentOn(w *rules.Engine) (decision.Intent, bool) {
	pd := w.Pending()
	if pd == nil {
		return decision.Intent{}, false
	}
	in := decision.Intent{Seq: pd.Seq, Player: pd.Player}
	switch a.kind {
	case "pass":
		for _, o := range pd.Options {
			if o.Kind == "pass" {
				in.Choices = []int{o.Index}
				return in, true
			}
		}
		return in, false
	case "option":
		in.Choices = []int{a.opt}
		return in, true
	case "payment":
		acts := w.EnsurePaymentActions()
		for i := range acts {
			if acts[i].Cast.Object == a.obj && acts[i].Label == a.label && len(acts[i].Plans) > 0 {
				in.Payment = &decision.PaymentSelection{ActionID: acts[i].ID, Plan: decision.ClonePaymentPlan(acts[i].Plans[0])}
				return in, true
			}
		}
		return in, false
	case "attackers", "blockers":
		in.Choices = append([]int(nil), a.choices...)
		return in, true
	}
	return in, false
}

// rollPolicy answers every rollout decision for both seats.
type rollPolicy struct {
	kind  string
	bots  [2]*seat.Bot
	tacs  [2]*builtins.Seat
	board botpolicy.Board
	look  builtins.CardLookup
}

func newRollPolicy(kind string, seed uint64, look builtins.CardLookup) *rollPolicy {
	rp := &rollPolicy{kind: kind, look: look, board: botpolicy.NewBoard(2)}
	for p := 0; p < 2; p++ {
		s := mix(seed ^ uint64(p+1)*0x9e37)
		if kind == "tactical" {
			rp.tacs[p] = builtins.NewTactical(builtins.AutoPay, s, look, builtins.DefaultTacticalWeights())
		} else {
			rp.bots[p] = seat.NewBot(s).EnableAutoPayMana()
		}
	}
	return rp
}

func (rp *rollPolicy) answer(e *rules.Engine) (decision.Intent, error) {
	pd := e.Pending()
	p := pd.Player
	if pd.Kind == decision.KPriority {
		e.EnsurePaymentActions()
		pd = e.Pending()
	}
	if rp.kind == "tactical" {
		t := rp.tacs[p]
		t.SetPlanner(e)
		v := view.Project(e.G, e, p, pd)
		v.Round = view.RoundOf(e.G, e.L.Events)
		return t.Decide(context.Background(), v, *pd)
	}
	b := botpolicy.BoardFromGameInto(e.G, e, p, &rp.board)
	return rp.bots[p].DecideBoard(context.Background(), b, *pd)
}

// passOrFirst is the minimal valid answer when a policy answer is refused.
func passOrFirst(pd *decision.Decision) decision.Intent {
	in := decision.Intent{Seq: pd.Seq, Player: pd.Player}
	for _, o := range pd.Options {
		if o.Kind == "pass" {
			in.Choices = []int{o.Index}
			return in
		}
	}
	if pd.Min == 0 {
		return in
	}
	for i := 0; i < pd.Min && i < len(pd.Options); i++ {
		in.Choices = append(in.Choices, pd.Options[i].Index)
	}
	return in
}

// rollout plays a from world w (consumed) and returns the leaf value for
// me, ok false when the rollout failed.
func (r *RollConfig) rollout(w *rules.Engine, me state.PlayerID, a *action, seed uint64, look builtins.CardLookup) (v float64, ok bool) {
	defer func() {
		if rec := recover(); rec != nil {
			v, ok = 0, false
		}
	}()
	in, found := a.intentOn(w)
	if !found {
		return 0, false
	}
	if err := w.SubmitHypothetical(in); err != nil {
		return 0, false
	}
	rp := newRollPolicy(r.Rollout, seed, look)
	horizon := w.G.Turn + int32(r.Horizon)
	for steps := 0; steps < r.MaxSteps; steps++ {
		if w.G.Over || w.G.Turn > horizon {
			break
		}
		pd := w.Pending()
		if pd == nil {
			return 0, false
		}
		ans, err := rp.answer(w)
		if err != nil {
			ans = passOrFirst(pd)
		}
		if err := w.SubmitHypothetical(ans); err != nil {
			if pd2 := w.Pending(); pd2 == nil || pd2.Seq != pd.Seq {
				return 0, false
			}
			if err := w.SubmitHypothetical(passOrFirst(pd)); err != nil {
				return 0, false
			}
		}
	}
	pv := view.Project(w.G, w, me, w.Pending())
	return searchprobe.LeafValue(pv, me), true
}

// rollResult is one candidate's evaluation.
type rollResult struct {
	sum float64
	n   int
}

func (r rollResult) mean() float64 {
	if r.n == 0 {
		return -1
	}
	return r.sum / float64(r.n)
}

// evaluate plays every action in the same W worlds of sh.
func (r *RollConfig) evaluate(sh *Shadow, acts []*action, seed uint64, look builtins.CardLookup) []rollResult {
	res := make([]rollResult, len(acts))
	for w := 0; w < r.Worlds; w++ {
		ws := mix(seed ^ mix(uint64(w)+0x77))
		base := sh.E.CloneHypothetical(ws)
		Redeal(base, sh, rand.New(rand.NewPCG(ws, ws^0x5eed)))
		for i, a := range acts {
			world := base.CloneHypothetical(mix(ws ^ 0xc0ffee))
			if v, ok := r.rollout(world, sh.Me, a, mix(ws^0xb0b), look); ok {
				res[i].sum += v
				res[i].n++
			}
		}
	}
	return res
}

// choose returns the index of the action to play (base is the fallback's
// action index) and a one-line summary.
func (r *RollConfig) choose(res []rollResult, base int) (int, string) {
	best := base
	for i := range res {
		if res[i].n*2 < r.Worlds {
			continue
		}
		if res[i].mean() > res[best].mean() {
			best = i
		}
	}
	if res[base].n*2 < r.Worlds {
		return base, "fallback candidate failed its rollouts"
	}
	if best != base && res[best].mean()-res[base].mean() <= r.Margin {
		best = base
	}
	s := ""
	for i := range res {
		s += fmt.Sprintf(" %.3f/%d", res[i].mean(), res[i].n)
	}
	return best, s
}

// topK keeps the k best-scored indices of idx (scores by kernel candidate)
// plus must, in score order.
func topK(idx []int, scores []float64, k int, must int) []int {
	sorted := append([]int(nil), idx...)
	sort.SliceStable(sorted, func(a, b int) bool {
		sa, sb := 0.0, 0.0
		if scores != nil {
			sa, sb = scores[sorted[a]], scores[sorted[b]]
		}
		return sa > sb
	})
	var out []int
	has := false
	for _, i := range sorted {
		if len(out) >= k {
			break
		}
		out = append(out, i)
		if i == must {
			has = true
		}
	}
	if !has {
		out = append(out, must)
	}
	return out
}
