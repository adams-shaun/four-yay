package decision

import "github.com/adams-shaun/gorge/state"

// blockRequiredCore finds the largest legal team of required blockers (CR
// 509.1c). Unlike a pairwise matching, a block of a Min$ N attacker is legal
// only when the rest of the team can join it. The same result supplies the
// engine's quota and the bot's repair; no separate board-side feasibility
// estimate can disagree with it. Called only on decisions with MustBlock.
//
// A requirement is either BLOCKER-oriented (Option.BlockMust/Required: a
// particular creature must block) or ATTACKER-oriented (Option.AttackMust: a
// particular attacker must receive at least one blocker, "CARDNAME must be
// blocked if able"). The team maximizes the TOTAL number of satisfied
// requirements, so a declaration that satisfies one of each always beats one
// that satisfies only the other; ties take the shortest team.
func (d *Decision) blockRequiredCore() []int {
	// No requirement of either orientation: no candidate blocker, so the
	// search below returns nil -- skip building its tables.
	if !d.hasRequiredBlocks() {
		return nil
	}
	type blocker struct {
		id       state.ObjID
		opts     []int
		required bool
	}
	var blocks []blocker
	pos := make(map[state.ObjID]int)
	minAttackers := make(map[state.ObjID]bool)
	// atkReq is the set of attackers carrying an attacker-oriented
	// requirement. Each is satisfied once by ANY chosen option naming it.
	atkReq := make(map[state.ObjID]bool)
	for _, o := range d.Options {
		if o.AttackMust {
			atkReq[o.Attacker] = true
		}
		if (o.BlockMust || o.Required || o.AttackMust) && o.MinBlockers > 1 {
			minAttackers[o.Attacker] = true
		}
	}
	for i, o := range d.Options {
		p, ok := pos[o.Obj]
		if !ok {
			p = len(blocks)
			pos[o.Obj] = p
			blocks = append(blocks, blocker{id: o.Obj})
		}
		blocks[p].required = blocks[p].required || o.BlockMust || o.Required
		blocks[p].opts = append(blocks[p].opts, i)
	}
	// Required creatures first; BlockMustAll options are separate required
	// blocker-attacker pairs, allowing one explicitly permitted blocker to
	// satisfy several defined attacker duties. Ordinary MustBlock options
	// remain one unit per blocker. Other blockers are helpers only for a
	// multi-blocker minimum or an attacker-oriented requirement.
	var candidates, helpers []blocker
	for _, b := range blocks {
		var ordinary blocker
		ordinary.id = b.id
		for _, i := range b.opts {
			o := d.Options[i]
			if o.BlockMustAll {
				candidates = append(candidates, blocker{id: b.id, opts: []int{i}, required: true})
				continue
			}
			if o.BlockMust || o.Required {
				ordinary.required = true
				ordinary.opts = append(ordinary.opts, i)
			} else if minAttackers[o.Attacker] || atkReq[o.Attacker] {
				ordinary.opts = append(ordinary.opts, i)
			}
		}
		if ordinary.required {
			candidates = append(candidates, ordinary)
		} else if len(ordinary.opts) != 0 {
			helpers = append(helpers, ordinary)
		}
	}
	requiredCount := len(candidates)
	candidates = append(candidates, helpers...)
	counts := make(map[state.ObjID]int)
	satAtk := make(map[state.ObjID]bool)
	lifeBound := d.PayerLifeBound()
	var chosen, best []int
	bestRequired := -1
	bestLength := int(^uint(0) >> 1)
	var search func(int, int, int, int32)
	search = func(at, satisfied, spent int, spentLife int32) {
		if bestRequired == requiredCount+len(atkReq) {
			return
		}
		remaining := requiredCount - at
		if remaining < 0 {
			remaining = 0
		}
		// Over-estimate the satisfaction still reachable from here: every
		// unsatisfied attacker requirement may still be satisfied ahead. An
		// over-estimate only costs search, never prunes a branch that could
		// beat the best team.
		if unsat := len(atkReq) - len(satAtk); unsat > 0 {
			remaining += unsat
		}
		if satisfied+remaining < bestRequired {
			return
		}
		if at == len(candidates) {
			for _, ci := range chosen {
				o := d.Options[ci]
				if o.MinBlockers > counts[o.Attacker] {
					return
				}
			}
			if satisfied > bestRequired || (satisfied == bestRequired && len(chosen) < bestLength) {
				bestRequired, bestLength = satisfied, len(chosen)
				best = append([]int(nil), chosen...)
			}
			return
		}
		b := candidates[at]
		try := func(ci int) {
			o := d.Options[ci]
			if len(chosen) >= d.maxChoices() || (d.HasBudget() && spent+o.Value > d.MaxSum) {
				return
			}
			// The combined non-mana LIFE charge bound is enforced INSIDE the
			// team search, not by pruning a finished team afterwards: a team
			// member dropped after the fact can break a Min$ team minimum (the
			// remaining members no longer form a legal declaration). One
			// option priced through the shared chargeLifeCost reader, so this
			// bound and the attack path's quota price a pip identically.
			cost := o.chargeLifeCost()
			if lifeBound >= 0 && spentLife+cost > lifeBound {
				return
			}
			if o.MaxBlockers > 0 && counts[o.Attacker] >= o.MaxBlockers {
				return
			}
			counts[o.Attacker]++
			chosen = append(chosen, ci)
			add := 0
			if b.required {
				add = 1
			}
			// An attacker-oriented requirement is satisfied by the FIRST
			// chosen option naming that attacker; later blockers of the same
			// attacker add nothing. Only the option that SET the flag clears
			// it on backtrack -- a second blocker of the same attacker must
			// not delete the first one's satisfaction.
			setAtk := o.AttackMust && !satAtk[o.Attacker]
			if setAtk {
				satAtk[o.Attacker] = true
				add++
			}
			search(at+1, satisfied+add, spent+o.Value, spentLife+cost)
			if setAtk {
				delete(satAtk, o.Attacker)
			}
			chosen = chosen[:len(chosen)-1]
			counts[o.Attacker]--
		}
		if b.required {
			// Prefer a legal singleton over a multi-blocker team when both
			// discharge the same requirement; this also keeps a Min$ attacker
			// available to an actual team if another required blocker needs it.
			for _, ci := range b.opts {
				if d.Options[ci].MinBlockers <= 1 {
					try(ci)
				}
			}
			for _, ci := range b.opts {
				if d.Options[ci].MinBlockers > 1 {
					try(ci)
				}
			}
			search(at+1, satisfied, spent, spentLife)
		} else {
			search(at+1, satisfied, spent, spentLife)
			for _, ci := range b.opts {
				try(ci)
			}
		}
	}
	search(0, 0, 0, 0)
	return best
}

func (d *Decision) hasRequiredBlocks() bool {
	for _, o := range d.Options {
		if o.BlockMust || o.Required || o.AttackMust {
			return true
		}
	}
	return false
}

// blockRequirementsSatisfied counts the CR 509.1c requirement units the
// chosen options satisfy: one per required BLOCKER chosen (BlockMust/Required)
// plus one per required ATTACKER that at least one chosen option blocks
// (AttackMust). This is the ONE counter blockRequiredQuota reads on the
// solver's team and RequiredChosen reads on a submitted answer, so the engine's
// declaration check and the client's repair can never disagree about what
// "the maximum" means. Out-of-range indices are ignored.
func (d *Decision) blockRequirementsSatisfied(choices []int) int {
	// Map-free (it runs per candidate answer in search): a unit counts at the
	// first choice that satisfies it, found by scanning the choices before.
	n := 0
	for k, c := range choices {
		if c < 0 || c >= len(d.Options) {
			continue
		}
		o := &d.Options[c]
		blockUnit, allPairUnit, atkUnit := !o.BlockMustAll && (o.BlockMust || o.Required), o.BlockMustAll, o.AttackMust
		for _, p := range choices[:k] {
			if !blockUnit && !allPairUnit && !atkUnit {
				break
			}
			if p < 0 || p >= len(d.Options) {
				continue
			}
			q := &d.Options[p]
			if blockUnit && !q.BlockMustAll && (q.BlockMust || q.Required) && q.Obj == o.Obj {
				blockUnit = false
			}
			if allPairUnit && q.BlockMustAll && q.Obj == o.Obj && q.Attacker == o.Attacker {
				allPairUnit = false
			}
			if atkUnit && q.AttackMust && q.Attacker == o.Attacker {
				atkUnit = false
			}
		}
		if blockUnit || allPairUnit {
			n++
		}
		if atkUnit {
			n++
		}
	}
	return n
}

func (d *Decision) blockRequiredQuota() int {
	return d.blockRequirementsSatisfied(d.blockRequiredCoreChargeFeasible())
}

// blockRequiredCoreChargeFeasible is the legal maximum team over all MustBlock
// candidates, with the combined non-mana LIFE charge (CostLife plus each
// Phyrexian pip at two life, the shared chargeLifeCost price) bounded INSIDE
// the team search. The charge bound is a whole-declaration property the
// per-pair option list cannot express, and it is solved WITH the team minima
// rather than by pruning a finished team: filtering a complete team pair by
// pair can drop a member a Min$ attacker needed and return a set that is not
// a legal declaration at all, which Submit rejects. blockRequiredCore already
// enforces the bound (and the mana budget) on every candidate, so its result
// is charge-feasible by construction; a charge-free decision (no published
// PayerLife) skips the check and is byte-identical. ONE derivation, so the
// quota and the repair cannot disagree.
func (d *Decision) blockRequiredCoreChargeFeasible() []int {
	return d.blockRequiredCore()
}

func (d *Decision) blockAnswerLegal(choices []int) bool {
	if len(choices) > d.maxChoices() || !d.blockCountLegal(choices) {
		return false
	}
	sum := 0
	groups := make(map[string]bool)
	for _, ci := range choices {
		o := d.Options[ci]
		if o.Group != "" && groups[o.Group] {
			return false
		}
		groups[o.Group] = true
		sum += o.Value
	}
	return !d.HasBudget() || sum <= d.MaxSum
}

// BlockRequiredTeam is the legal maximum team over all MustBlock candidates.
// Rules uses it when constructing the offer to highlight one feasible team.
func (d *Decision) BlockRequiredTeam() []int {
	return d.blockRequiredCoreChargeFeasible()
}

// blockCountLegal checks published per-attacker team minima and maxima on an
// already selected answer. The engine still checks its live board bounds.
func (d *Decision) blockCountLegal(choices []int) bool {
	counts := make(map[state.ObjID]int)
	for _, ci := range choices {
		if ci < 0 || ci >= len(d.Options) {
			return false
		}
		counts[d.Options[ci].Attacker]++
	}
	for _, ci := range choices {
		o := d.Options[ci]
		if counts[o.Attacker] < o.MinBlockers || (o.MaxBlockers > 0 && counts[o.Attacker] > o.MaxBlockers) {
			return false
		}
	}
	return true
}
