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
	var scores []float64
	if t, ok := p.fb.(*v1agent.Tactical); ok {
		scores = t.Scores(d)
	}
	idx = topK(idx, scores, p.cfg.Roll.TopK, fbPick)
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
	res := p.cfg.Roll.evaluate(sh, list, p.decisionSeed(d), p.lookup())
	p.rollStats(res)
	best, summary := p.cfg.Roll.choose(res, base)
	if best != base {
		p.Roll.Overrides++
	}
	p.lastRoll = summary
	return idx[best], true, ""
}

func (p *Policy) rollStats(res []rollResult) {
	p.Roll.Searched++
	for _, r := range res {
		p.Roll.Rollouts += p.cfg.Roll.Worlds
		p.Roll.Failed += p.cfg.Roll.Worlds - r.n
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
		add(nil)
		add(all)
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
		add(nil)
	}
	// The gorge bot's and sb-tactical's declarations.
	brd := botpolicy.NewBoard(2)
	b := botpolicy.BoardFromGameInto(sh.E.G, sh.E, sh.Me, &brd)
	if in, err := seat.NewBot(p.decisionSeed(d)^0xc0b).DecideBoard(context.Background(), b, *pd); err == nil {
		add(in.Choices)
	}
	if in, err := p.tacticalAnswer(sh); err == nil && in.Payment == nil {
		add(in.Choices)
	}
	acts := make([]*action, len(plans))
	for i, pl := range plans {
		acts[i] = &action{kind: kind, choices: pl}
	}
	if len(acts) < 2 {
		return decision.Intent{Seq: pd.Seq, Player: pd.Player, Choices: plans[0]}, nil
	}
	res := p.cfg.Roll.evaluate(sh, acts, p.decisionSeed(d), p.lookup())
	p.rollStats(res)
	best, summary := p.cfg.Roll.choose(res, 0)
	if best != 0 {
		p.Roll.Overrides++
	}
	p.lastRoll = summary
	if res[0].n*2 < p.cfg.Roll.Worlds && best == 0 {
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
