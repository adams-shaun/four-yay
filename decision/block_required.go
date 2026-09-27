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
	// Required creatures first; ordinary blockers need only be considered
	// as helpers for a required block with a multi-blocker minimum, or as a
	// satisfier of an attacker requirement.
	var candidates []blocker
	for _, b := range blocks {
		if b.required {
			candidates = append(candidates, b)
		}
	}
	requiredCount := len(candidates)
	for _, b := range blocks {
		if b.required {
			continue
		}
		var helper blocker
		helper.id = b.id
		for _, i := range b.opts {
			o := d.Options[i]
			if minAttackers[o.Attacker] || atkReq[o.Attacker] {
				helper.opts = append(helper.opts, i)
			}
		}
		if len(helper.opts) != 0 {
			candidates = append(candidates, helper)
		}
	}
	counts := make(map[state.ObjID]int)
	satAtk := make(map[state.ObjID]bool)
	var chosen, best []int
	bestRequired := -1
	bestLength := int(^uint(0) >> 1)
	var search func(int, int, int)
	search = func(at, satisfied, spent int) {
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
			search(at+1, satisfied+add, spent+o.Value)
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
			search(at+1, satisfied, spent)
		} else {
			search(at+1, satisfied, spent)
			for _, ci := range b.opts {
				try(ci)
			}
		}
	}
	search(0, 0, 0)
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
	seenBlocker := make(map[state.ObjID]bool)
	seenAttacker := make(map[state.ObjID]bool)
	n := 0
	for _, c := range choices {
		if c < 0 || c >= len(d.Options) {
			continue
		}
		o := d.Options[c]
		if (o.BlockMust || o.Required) && !seenBlocker[o.Obj] {
			seenBlocker[o.Obj] = true
			n++
		}
		if o.AttackMust && !seenAttacker[o.Attacker] {
			seenAttacker[o.Attacker] = true
			n++
		}
	}
	return n
}

func (d *Decision) blockRequiredQuota() int {
	return d.blockRequirementsSatisfied(d.blockRequiredCoreChargeFeasible())
}

// blockRequiredCoreChargeFeasible is blockRequiredCore filtered through the
// SAME combined non-mana charge rule the attack path uses
// (ChargeOptionConstraints over the decision's published PayerLife): a legal
// team that leaves the whole declaration unpayable is not a team the engine's
// combined-charge check accepts, so it must not be counted by the quota nor
// rebuilt by FitRequired. The unfiltered core is returned unchanged whenever
// it is already payable, so every charge-free block decision is
// byte-identical. ONE derivation, so the quota and the repair cannot
// disagree.
func (d *Decision) blockRequiredCoreChargeFeasible() []int {
	core := d.blockRequiredCore()
	if d.ChargeOptionsFit(core) {
		return core
	}
	return ChargeOptionConstraints(d, core, d.PayerLifeBound(), 0)
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
