package decision

import (
	"sort"

	"github.com/adams-shaun/gorge/state"
)

// The Required-quota contract. Option.Required marks an option whose Obj the
// answer MUST include (CR 508.1d's "attacks if able" on a KAttackers pair
// list), and Decision.MaxSum caps the chosen options' Value total (the
// CantAttackUnless attack-prop price). The two constraints interact: a
// required creature is "able" to attack only while the declaration stays
// within budget, so the number of required Objs an answer must cover is not
// "all of them" but the most the budget and Max can carry.
//
// That number is ONE rule, stated here and read by both sides: the engine's
// declaration check (rules' validateAttackDeclaration) demands exactly
// RequiredQuota distinct required Objs, and every client repair (botpolicy's
// Clamp) builds its answer from FitRequired, which starts from the very set
// RequiredQuota counts. So no answer a client assembles by this rule can be
// rejected for a requirement, and no requirement can outrun what the budget
// lets a client choose. A second, independent derivation on either side is
// how an engine-satisfiable decision became a bot livelock (attackprop1
// review: the engine priced the requirement from each creature's CHEAPEST
// pair while the bot's trim dropped required picks priced on DEARER ones).

// requiredCore returns, for each distinct Obj carrying a Required option, its
// cheapest Required option (ties: lowest option index), ordered by ascending
// Value and then by the Obj's first-seen option position; then it keeps the
// longest prefix whose Values fit MaxSum (when > 0) and whose length fits Max.
// Ascending-price greedy is optimal for the count: no other choice of one
// option per Obj covers more Objs within the same budget.
func (d *Decision) requiredCore() []int {
	type pick struct{ idx, pos int }
	best := make(map[state.ObjID]*pick) // membership/lookup only -- never ranged.
	var order []*pick
	for i := range d.Options {
		o := &d.Options[i]
		if !o.Required {
			continue
		}
		p, ok := best[o.Obj]
		if !ok {
			p = &pick{idx: i, pos: i}
			best[o.Obj] = p
			order = append(order, p)
			continue
		}
		if o.Value < d.Options[p.idx].Value {
			p.idx = i
		}
	}
	sort.SliceStable(order, func(a, b int) bool {
		va, vb := d.Options[order[a].idx].Value, d.Options[order[b].idx].Value
		if va != vb {
			return va < vb
		}
		return order[a].pos < order[b].pos
	})
	var core []int
	sum := 0
	for _, p := range order {
		if len(core) >= d.maxChoices() {
			break
		}
		v := d.Options[p.idx].Value
		if d.HasBudget() && sum+v > d.MaxSum {
			break // ascending: nothing later fits either
		}
		sum += v
		core = append(core, p.idx)
	}
	return core
}

// RequiredQuota is how many distinct Required Objs a valid answer must
// include: the most the decision's MaxSum budget and Max ceiling can carry,
// cheapest Required option per Obj first. KBlockers uses the whole legal
// blocking-team solver instead. 0 when no duty is present.
func (d *Decision) RequiredQuota() int {
	if d.Kind == KBlockers {
		return d.blockRequiredQuota()
	}
	return len(d.requiredCore())
}

// RequiredChosen counts the distinct required Objs chosen. KBlockers uses the
// whole-declaration requirement counter instead (blockRequirementsSatisfied),
// because a blocking answer can satisfy an ATTACKER-oriented requirement with
// any one of several blocker pairs, and counts required BLOCKERS and required
// ATTACKERS alike. Out-of-range indices are ignored.
func (d *Decision) RequiredChosen(choices []int) int {
	if d.Kind == KBlockers {
		return d.blockRequirementsSatisfied(choices)
	}
	seen := make(map[state.ObjID]bool, len(choices)) // membership only.
	n := 0
	for _, c := range choices {
		if c < 0 || c >= len(d.Options) || !d.Options[c].Required {
			continue
		}
		if obj := d.Options[c].Obj; !seen[obj] {
			seen[obj] = true
			n++
		}
	}
	return n
}

// FitRequired returns an answer that satisfies the Max ceiling, the MaxSum
// budget and the RequiredQuota at once, keeping as much of choices (a
// client's preferred answer, in its order) as those allow. When choices
// already satisfies all three it is returned unchanged, so a decision with no
// budget and no requirement pressure is byte-identical.
//
// Otherwise the answer is rebuilt from the requiredCore -- exactly the set
// RequiredQuota counts, so the quota is met by construction -- and then each
// preferred choice, in order, is folded in while it fits: a choice whose Obj
// is already covered by a Required pick REPLACES that pick when the swap
// stays within budget (one option per Obj: an attacker attacks one defender),
// any other choice is appended while the budget, Max and option Groups allow.
// Neither step can lower the count of Required Objs, so the rebuilt answer
// keeps the quota.
func (d *Decision) FitRequired(choices []int) []int {
	if d.Kind == KBlockers && d.hasRequiredBlocks() {
		core := d.blockRequiredCore()
		// An already legal preferred declaration retains its damage-order
		// choice. Otherwise the same legal team that sets the quota repairs it.
		if d.blockAnswerLegal(choices) && d.RequiredChosen(choices) >= d.RequiredQuota() {
			return choices
		}
		return core
	}
	sum := 0
	for _, c := range choices {
		if c >= 0 && c < len(d.Options) {
			sum += d.Options[c].Value
		}
	}
	if len(choices) <= d.maxChoices() &&
		(!d.HasBudget() || sum <= d.MaxSum) &&
		(d.MinSum <= 0 || sum >= d.MinSum) &&
		d.RequiredChosen(choices) >= d.RequiredQuota() &&
		!d.groupCapExceeded(choices) &&
		d.setPropAnswerAdmits(choices) {
		return choices
	}

	out := d.requiredCore()
	sum = 0
	slotOf := make(map[state.ObjID]int, len(out)) // Obj -> position in out.
	have := make(map[int]bool, len(out)+len(choices))
	// groups counts the picked options per Group against GroupCapFor -- the
	// same cap Decision.Validate enforces, so a repaired answer can never be
	// one Validate rejects. At the default cap of 1 a nonzero count is the
	// historical boolean "already represented", so every limit-free decision
	// repairs byte-identically.
	groups := make(map[string]int)
	objTaken := make(map[state.ObjID]bool, len(out)) // membership only.
	for i, c := range out {
		objTaken[d.Options[c].Obj] = true
		sum += d.Options[c].Value
		slotOf[d.Options[c].Obj] = i
		have[c] = true
		if g := d.Options[c].Group; g != "" {
			groups[g]++
		}
	}
	requiredObj := make(map[state.ObjID]bool)
	for i := range d.Options {
		if d.Options[i].Required {
			requiredObj[d.Options[i].Obj] = true
		}
	}
	// setAcc tracks the running target-set property accumulator (the same
	// SetPropAdmits/SetPropMerge rule Validate enforces), so the fold can
	// never append an option the set constraint refuses.
	setAcc := d.setPropAccumulator(out)
	fits := func(delta int) bool { return !d.HasBudget() || sum+delta <= d.MaxSum }
	for _, c := range choices {
		if c < 0 || c >= len(d.Options) || (have[c] && !d.Repeatable) {
			continue
		}
		o := &d.Options[c]
		if requiredObj[o.Obj] {
			if slot, ok := slotOf[o.Obj]; ok {
				old := &d.Options[out[slot]]
				if (o.Group != "" && o.Group != old.Group && groups[o.Group] >= d.GroupCapFor(o.Group)) || !fits(o.Value-old.Value) {
					continue
				}
				sum += o.Value - old.Value
				delete(have, out[slot])
				if old.Group != "" {
					groups[old.Group]--
				}
				out[slot] = c
				have[c] = true
				if o.Group != "" {
					groups[o.Group]++
				}
				continue
			}
		}
		if len(out) >= d.maxChoices() {
			continue
		}
		// KAttackers: one pair per creature (CR 506.2; the engine rejects a
		// creature declared twice), so a second pair of an Obj already in
		// the answer is skipped like a second member of an option Group.
		if d.Kind == KAttackers && objTaken[o.Obj] {
			continue
		}
		if (o.Group != "" && groups[o.Group] >= d.GroupCapFor(o.Group)) || !fits(o.Value) ||
			!SetPropAdmits(d.SetPropMode, setAcc, o.SetProps) {
			continue
		}
		sum += o.Value
		setAcc = SetPropMerge(d.SetPropMode, setAcc, o.SetProps)
		out = append(out, c)
		have[c] = true
		objTaken[o.Obj] = true
		if o.Group != "" {
			groups[o.Group]++
		}
		if requiredObj[o.Obj] {
			slotOf[o.Obj] = len(out) - 1
		}
	}
	// A budget of zero or less (Budgeted, MaxTotalTargetPower$ <= 0) can
	// leave even the rebuilt answer over it: the empty answer totals 0,
	// which busts a negative cap, and the fold above skips a negative option
	// whose own offset is not enough on its own. Every negative-Value option
	// strictly lowers the total, so take the unused ones, most negative
	// first, until the answer fits (Max, Groups and the one-pair-per-creature
	// rule still apply). The engine offers such a decision only when taking
	// every negative option fits (rules' totalPowerCappedCandidates prunes
	// the whole census otherwise), so this reaches a valid answer whenever
	// one exists. A positive budget never reaches this: the rebuild starts
	// within it and the fold only appends what fits.
	if d.HasBudget() && sum > d.MaxSum {
		var neg []int
		for i := range d.Options {
			if d.Options[i].Value < 0 && !have[i] {
				neg = append(neg, i)
			}
		}
		sort.SliceStable(neg, func(a, b int) bool { return d.Options[neg[a]].Value < d.Options[neg[b]].Value })
		for _, c := range neg {
			if sum <= d.MaxSum || len(out) >= d.maxChoices() {
				break
			}
			o := &d.Options[c]
			if (o.Group != "" && groups[o.Group] >= d.GroupCapFor(o.Group)) || (d.Kind == KAttackers && objTaken[o.Obj]) {
				continue
			}
			sum += o.Value
			out = append(out, c)
			have[c] = true
			objTaken[o.Obj] = true
			if o.Group != "" {
				groups[o.Group]++
			}
		}
	}
	// The cumulative floor (Decision.MinSum, a withTotalPowerGE<N> group
	// predicate's "total power N or greater"): if the rebuilt answer's sum
	// falls short, top up with the remaining options of highest Value first
	// -- Max, Groups and the one-pair-per-creature rule still apply. The
	// engine poses a floor ask only when a satisfying set exists within the
	// offered options (the offer gate proved the candidates can reach the
	// floor), so this reaches a valid answer whenever one exists. Highest
	// Value first keeps the repair deterministic and spends the fewest
	// picks, and the sum is order-insensitive, so how equal Values sort in
	// cannot reach an event.
	if d.MinSum > 0 && sum < d.MinSum {
		var rest []int
		for i := range d.Options {
			if !have[i] {
				rest = append(rest, i)
			}
		}
		sort.SliceStable(rest, func(a, b int) bool { return d.Options[rest[a]].Value > d.Options[rest[b]].Value })
		for _, c := range rest {
			if sum >= d.MinSum || len(out) >= d.maxChoices() {
				break
			}
			o := &d.Options[c]
			if (o.Group != "" && groups[o.Group] >= d.GroupCapFor(o.Group)) || (d.Kind == KAttackers && objTaken[o.Obj]) {
				continue
			}
			sum += o.Value
			out = append(out, c)
			have[c] = true
			objTaken[o.Obj] = true
			if o.Group != "" {
				groups[o.Group]++
			}
		}
	}
	return out
}

// maxChoices is Max floored at 0: Validate rejects any answer longer than a
// negative Max, so the repair treats it as "choose nothing".
func (d *Decision) maxChoices() int {
	if d.Max < 0 {
		return 0
	}
	return d.Max
}
