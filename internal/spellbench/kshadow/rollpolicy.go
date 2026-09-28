package kshadow

import (
	"context"
	"fmt"
	"sort"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/spellbench/builtins"
	"github.com/adams-shaun/gorge/internal/spellbench/v1agent"
	"github.com/adams-shaun/gorge/seat"
	"github.com/adams-shaun/gorge/state"
)

// RollStats counts the rollout search's work.
type RollStats struct {
	Searched, Overrides, Rollouts, Failed int
	// Proposals counts the arbiter's second opinions (sb-tactical picks
	// mapped to a kernel candidate); Searched those that disagreed.
	Proposals int
}

// clone copies the shadow onto an independent engine (the policy's own
// lookahead must not disturb the root the rollouts start from).
func (sh *Shadow) clone() *Shadow {
	c := *sh
	c.E = sh.E.Clone()
	return &c
}

// tacticalPick is sb-tactical's answer on a copy of the shadow, as a
// kernel candidate index.
func (p *Policy) tacticalPick(sh *Shadow, d *v1agent.Decision) (int, *followPlan, bool) {
	c := sh.clone()
	in, err := p.tacticalAnswer(c)
	if err != nil {
		return 0, nil, false
	}
	in, pd, err := throughMana(c, in, func() (decision.Intent, error) { return p.tacticalAnswer(c) })
	if err != nil {
		return 0, nil, false
	}
	k, _ := kernelIndexPriority(c, d, pd, in)
	if k < 0 {
		return k, nil, false
	}
	return k, p.makePlanWith(c, pd, in, p.decisionSeed(d), true), true
}

func (p *Policy) lookup() builtins.CardLookup {
	if p.look == nil {
		p.look = builtins.NewRegistryLookup(p.cfg.Reg)
	}
	return p.look
}

// rollPriority is ModeRoll at a priority decision.
func (p *Policy) rollPriority(sh *Shadow, d *v1agent.Decision, fbPick int) (int, bool, string) {
	m := MapDecision(sh, d)
	pd := sh.E.Pending()
	var idx []int
	acts := map[int]*action{}
	for i, g := range m.Gorge {
		if m.Mana[i] || g < 0 {
			continue
		}
		var a *action
		switch {
		case d.Candidates[i].Kind() == "pass":
			a = &action{kind: "pass"}
		case g >= PotentialBase:
			continue // reachable only through mana activations
		case g >= PaymentBase:
			pa := pd.PaymentActions[g-PaymentBase]
			a = &action{kind: "payment", obj: pa.Cast.Object, label: pa.Label}
		default:
			a = &action{kind: "option", opt: g}
		}
		acts[i] = a
		idx = append(idx, i)
	}
	if acts[fbPick] == nil {
		return 0, false, "roll: fallback pick not searchable"
	}
	if p.roll.Arbiter {
		alt, altPlan, ok := p.tacticalPick(sh, d)
		if !ok || acts[alt] == nil {
			return fbPick, true, ""
		}
		p.Roll.Proposals++
		if alt == fbPick {
			return fbPick, true, ""
		}
		idx = []int{fbPick, alt}
		if p.roll.Extra > 0 && !p.roll.BaseTactical {
			if t, ok := p.fb.(*v1agent.Tactical); ok {
				var rest []int
				for i := range acts {
					if i != fbPick && i != alt {
						rest = append(rest, i)
					}
				}
				sort.Ints(rest)
				more := topK(rest, t.Scores(d), p.roll.Extra, -1)
				for _, i := range more {
					if i >= 0 && acts[i] != nil {
						idx = append(idx, i)
					}
				}
			}
		}
		if p.roll.BaseTactical {
			idx = []int{alt, fbPick}
			base := 0
			list := []*action{acts[alt], acts[fbPick]}
			res := p.roll.evaluate(sh, list, p.decisionSeed(d), p.lookup())
			p.rollStats(res)
			best, summary := p.roll.choose(res, base)
			if best != base {
				p.Roll.Overrides++
			} else {
				p.plan = altPlan
			}
			p.lastRoll = summary
			return idx[best], true, ""
		}
		p.altPlan = altPlan
	} else {
		var scores []float64
		if t, ok := p.fb.(*v1agent.Tactical); ok {
			scores = t.Scores(d)
		}
		idx = topK(idx, scores, p.roll.TopK, fbPick)
	}
	if len(idx) < 2 {
		return fbPick, true, ""
	}
	list := make([]*action, len(idx))
	base := 0
	for j, i := range idx {
		list[j] = acts[i]
		if i == fbPick {
			base = j
		}
	}
	res := p.roll.evaluate(sh, list, p.decisionSeed(d), p.lookup())
	p.rollStats(res)
	best, summary := p.roll.choose(res, base)
	if best != base {
		p.Roll.Overrides++
	}
	if p.roll.Arbiter && idx[best] != fbPick {
		p.plan = p.altPlan
	}
	p.altPlan = nil
	p.lastRoll = summary
	return idx[best], true, ""
}

func (p *Policy) rollStats(res []rollResult) {
	p.Roll.Searched++
	for _, r := range res {
		p.Roll.Rollouts += p.roll.Worlds
		p.Roll.Failed += p.roll.Worlds - r.n
	}
}

// rollCombat evaluates candidate declarations at the shadow's KAttackers or
// KBlockers decision: the fallback's plan (the default), none, the gorge
// bot's, sb-tactical's and (attacks) everyone.
func (p *Policy) rollCombat(sh *Shadow, d *v1agent.Decision) (decision.Intent, error) {
	pd := sh.E.Pending()
	t, _ := p.fb.(*v1agent.Tactical)
	var plans [][]int
	add := func(ch []int) {
		ch = append([]int(nil), ch...)
		sort.Ints(ch)
		for _, q := range plans {
			if equalInts(q, ch) {
				return
			}
		}
		plans = append(plans, ch)
	}
	kind := "attackers"
	if pd.Kind == decision.KAttackers {
		var tac []int
		var plan map[uint32]bool
		if t != nil {
			plan = t.AttackPlan(d.Group.GroupID)
		}
		seen := map[state.ObjID]bool{}
		var all []int
		for _, o := range pd.Options {
			a, ok := sh.arenaOf(o.Obj)
			if !ok || seen[o.Obj] {
				continue
			}
			seen[o.Obj] = true
			all = append(all, o.Index)
			if plan[a] {
				tac = append(tac, o.Index)
			}
		}
		add(tac)
		if !p.roll.Arbiter {
			add(nil)
			add(all)
		}
	} else {
		kind = "blockers"
		var tac []int
		var plan map[uint32]uint32
		if t != nil {
			plan = t.BlockPlan(d.Group.GroupID)
		}
		for _, o := range pd.Options {
			b, ok1 := sh.arenaOf(o.Obj)
			a, ok2 := sh.arenaOf(o.Attacker)
			if ok1 && ok2 && plan != nil {
				if at, ok := plan[b]; ok && at == a {
					tac = append(tac, o.Index)
				}
			}
		}
		add(tac)
		if !p.roll.Arbiter {
			add(nil)
		}
	}
	// The gorge bot's and sb-tactical's declarations.
	if !p.roll.Arbiter {
		brd := botpolicy.NewBoard(2)
		b := botpolicy.BoardFromGameInto(sh.E.G, sh.E, sh.Me, &brd)
		if in, err := seat.NewBot(p.decisionSeed(d)^0xc0b).DecideBoard(context.Background(), b, *pd); err == nil {
			add(in.Choices)
		}
	}
	stBase := 0
	if in, err := p.tacticalAnswer(sh.clone()); err == nil && in.Payment == nil {
		add(in.Choices)
		ch := append([]int(nil), in.Choices...)
		sort.Ints(ch)
		for i, q := range plans {
			if equalInts(q, ch) {
				stBase = i
			}
		}
	}
	acts := make([]*action, len(plans))
	for i, pl := range plans {
		acts[i] = &action{kind: kind, choices: pl}
	}
	if len(acts) < 2 {
		return decision.Intent{Seq: pd.Seq, Player: pd.Player, Choices: plans[0]}, nil
	}
	res := p.roll.evaluate(sh, acts, p.decisionSeed(d), p.lookup())
	p.rollStats(res)
	base := 0
	if p.roll.BaseTactical {
		base = stBase
	}
	best, summary := p.roll.choose(res, base)
	if best != base {
		p.Roll.Overrides++
	}
	p.lastRoll = summary
	if res[base].n == 0 && best == base {
		return decision.Intent{}, fmt.Errorf("roll: every declaration failed")
	}
	return decision.Intent{Seq: pd.Seq, Player: pd.Player, Choices: plans[best]}, nil
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
