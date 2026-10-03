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
//
// The core is also COMBINED-CHARGE-FEASIBLE: the greedy carries the same
// cumulative LIFE bound (CostLife plus each Phyrexian pip at two life) that
// ChargeOptionConstraints enforces, and prices each option through the same
// chargeLifeCost reader, so the quota and the pre-filter cannot disagree about
// what a declaration costs in life. Without this, a required creature carrying
// an unpayable per-attacker tax was counted by RequiredQuota and restored by
// FitRequired, so the bot re-declared a whole declaration the engine's
// combined-charge check rejects -- the crash this rule exists to prevent. The
// per-Obj pick prefers the lower-life option on a Value tie so the core is as
// charge-cheap as the wire allows. A tapXType obligation is not priced here:
// ChargeOptionConstraints drops such an option outright (the wire cannot
// verify it), while the required set leaves it to the engine's board-aware
// payment -- the pre-existing contract.
func (d *Decision) requiredCore() []int {
	// No Required option: both the greedy and the knapsack below pick
	// nothing and return nil, so skip building their tables.
	if !d.anyRequired() {
		return nil
	}
	type pick struct{ idx, pos int }
	best := make(map[state.ObjID]*pick) // membership/lookup only -- never ranged.
	var order []*pick
	better := func(a, b int) bool {
		// Lower Value first; on a tie the lower non-mana life charge, then
		// the earlier option. Deterministic: no map iteration reaches the
		// comparison.
		oa, ob := &d.Options[a], &d.Options[b]
		if oa.Value != ob.Value {
			return oa.Value < ob.Value
		}
		ca := oa.chargeLifeCost()
		cb := ob.chargeLifeCost()
		return ca < cb
	}
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
		if better(i, p.idx) {
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
	life := d.PayerLifeBound()
	if life < 0 {
		// No published charge bound: the ascending-Value prefix greedy is
		// optimal for the count (the pre-charge contract) and byte-identical.
		//
		// The per-Group caps (Decision.GroupLimits, an AttackRestrict
		// ceiling) are restrictions too: CR 508.1d maximises requirements
		// WITHOUT violating them, so a pick whose Group is full is replaced
		// by the Obj's cheapest Required option in a Group with room, or the
		// Obj is not required at all. Without this the quota counted three
		// required attackers at a two-attacker ceiling and no declaration
		// could satisfy both it and Validate (cardfuzz fuzz-1003). A
		// decision without Groups never enters the replacement, so it stays
		// byte-identical.
		var core []int
		var tbuf [8]groupTally
		tally := tbuf[:0]
		sum := 0
		for _, p := range order {
			if len(core) >= d.maxChoices() {
				break
			}
			idx := p.idx
			if !groupTallyAdmits(d, tally, d.Options[idx].Group) {
				if idx = d.requiredInOpenGroup(d.Options[idx].Obj, tally, sum); idx < 0 {
					continue
				}
			} else if v := d.Options[idx].Value; d.HasBudget() && sum+v > d.MaxSum {
				break // ascending: nothing later fits either
			}
			sum += d.Options[idx].Value
			tally = groupTallyAdd(tally, d.Options[idx].Group)
			core = append(core, idx)
		}
		return core
	}

	// With a published life bound, this is a two-resource, multiple-choice
	// knapsack: choose at most one Required option per Obj, maximize count,
	// and respect life and (when present) Value budgets. Keep the cheapest
	// Value for each exact (life,count) state; for equal costs, keep the
	// lexicographically first option sequence. The sparse DP is
	// pseudopolynomial in the bounded life capacity and polynomial in the
	// number of objects/options, unlike exhaustive subset search.
	type key struct {
		life  int32
		count int
	}
	type candidate struct {
		value int
		picks []int
	}
	states := map[key]candidate{{}: {}}
	maxCount := d.maxChoices()
	lessPicks := func(a, b []int) bool {
		for i := 0; i < len(a) && i < len(b); i++ {
			if a[i] != b[i] {
				return a[i] < b[i]
			}
		}
		return len(a) < len(b)
	}
	groups := make([][]int, 0, len(order))
	gpos := make(map[state.ObjID]int, len(order))
	for i := range d.Options {
		o := &d.Options[i]
		if !o.Required {
			continue
		}
		g, ok := gpos[o.Obj]
		if !ok {
			g = len(groups)
			gpos[o.Obj] = g
			groups = append(groups, nil)
		}
		groups[g] = append(groups[g], i)
	}
	for _, group := range groups {
		next := make(map[key]candidate, len(states)*(len(group)+1))
		for k, c := range states {
			// Skipping this Obj is always an available transition. Preserve
			// the same minimum-Value/lexicographic dominance as option picks:
			// map iteration can otherwise overwrite a cheaper candidate.
			if old, exists := next[k]; !exists || c.value < old.value ||
				(c.value == old.value && lessPicks(c.picks, old.picks)) {
				next[k] = c
			}
			if k.count >= maxCount {
				continue
			}
			for _, ci := range group {
				o := &d.Options[ci]
				cost := o.chargeLifeCost()
				if cost > life-k.life || (d.HasBudget() && (o.Value > d.MaxSum-c.value)) {
					continue
				}
				nk := key{life: k.life + cost, count: k.count + 1}
				nc := candidate{value: c.value + o.Value, picks: append(append([]int(nil), c.picks...), ci)}
				old, exists := next[nk]
				if !exists || nc.value < old.value || (nc.value == old.value && lessPicks(nc.picks, old.picks)) {
					next[nk] = nc
				}
			}
		}
		states = next
	}

	var bestKey key
	var bestCandidate candidate
	found := false
	for k, c := range states {
		if !found || k.count > bestKey.count ||
			(k.count == bestKey.count && (k.life < bestKey.life ||
				(k.life == bestKey.life && (c.value < bestCandidate.value ||
					(c.value == bestCandidate.value && lessPicks(c.picks, bestCandidate.picks)))))) {
			bestKey, bestCandidate, found = k, c, true
		}
	}
	// The knapsack does not carry the per-Group caps in its state; drop any
	// pick past its Group's cap (in pick order) so the core -- and so the
	// quota the engine enforces -- is always an answer Validate accepts.
	picks := bestCandidate.picks
	var tbuf [8]groupTally
	tally := tbuf[:0]
	kept := picks[:0:0]
	for _, ci := range picks {
		g := d.Options[ci].Group
		if !groupTallyAdmits(d, tally, g) {
			continue
		}
		tally = groupTallyAdd(tally, g)
		kept = append(kept, ci)
	}
	if len(kept) == len(picks) {
		return picks
	}
	return kept
}

// groupTallyAdmits reports whether one more option of Group g fits g's cap
// given the running tally (GroupCapFor, the rule Validate enforces). The empty
// Group is uncapped.
func groupTallyAdmits(d *Decision, tally []groupTally, g string) bool {
	if g == "" {
		return true
	}
	n := 0
	if i := groupTallyAt(tally, g); i >= 0 {
		n = tally[i].count
	}
	return d.groupAdmitsCount(g, n)
}

// groupTallyAdd counts one more option of Group g.
func groupTallyAdd(tally []groupTally, g string) []groupTally {
	if g == "" {
		return tally
	}
	if i := groupTallyAt(tally, g); i >= 0 {
		tally[i].count++
		return tally
	}
	return append(tally, groupTally{g: g, count: 1})
}

// requiredInOpenGroup returns obj's cheapest Required option (ties: lowest
// index) whose Group still has room under tally and whose Value fits the
// MaxSum budget past sum, or -1.
func (d *Decision) requiredInOpenGroup(obj state.ObjID, tally []groupTally, sum int) int {
	best := -1
	for i := range d.Options {
		o := &d.Options[i]
		if !o.Required || o.Obj != obj || !groupTallyAdmits(d, tally, o.Group) {
			continue
		}
		if d.HasBudget() && sum+o.Value > d.MaxSum {
			continue
		}
		if best < 0 || o.Value < d.Options[best].Value {
			best = i
		}
	}
	return best
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
	// Map-free (it runs per candidate answer in search): an Obj counts at
	// its first required choice, found by scanning the choices before it.
	n := 0
	for k, c := range choices {
		if !d.requiredAt(c) {
			continue
		}
		obj, first := d.Options[c].Obj, true
		for _, p := range choices[:k] {
			if d.requiredAt(p) && d.Options[p].Obj == obj {
				first = false
				break
			}
		}
		if first {
			n++
		}
	}
	return n
}

// requiredAt reports whether c names an in-range Required option.
func (d *Decision) requiredAt(c int) bool {
	return c >= 0 && c < len(d.Options) && d.Options[c].Required
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
		core := d.blockRequiredCoreChargeFeasible()
		// An already legal preferred declaration retains its damage-order
		// choice. Otherwise the same legal team that sets the quota repairs it.
		if d.blockAnswerLegal(choices) && d.RequiredChosen(choices) >= d.RequiredQuota() && d.ChargeOptionsFit(choices) {
			return choices
		}
		return core
	}
	sum, sum2 := 0, 0
	for _, c := range choices {
		if c >= 0 && c < len(d.Options) {
			sum += d.Options[c].Value
			sum2 += d.Options[c].Value2
		}
	}
	if len(choices) <= d.maxChoices() &&
		(!d.HasBudget() || sum <= d.MaxSum) &&
		(!d.HasBudget2() || sum2 <= d.MaxSum2) &&
		(d.MinSum <= 0 || sum >= d.MinSum) &&
		d.RequiredChosen(choices) >= d.RequiredQuota() &&
		d.ChargeOptionsFit(choices) &&
		!d.groupCapExceeded(choices) &&
		d.setPropAnswerAdmits(choices) {
		return choices
	}

	out := d.requiredCore()
	sum, sum2 = 0, 0
	life := d.PayerLifeBound()
	spentLife := int32(0)
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
		sum2 += d.Options[c].Value2
		spentLife += d.Options[c].chargeLifeCost()
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
	fits := func(delta, delta2 int) bool { return d.BudgetsFit(sum+delta, sum2+delta2) }
	// chargeFits reports whether folding option `add` in while removing
	// `remove` (an option already in out, or -1 for an append) keeps the
	// combined non-mana charge within the published bound -- the same rule
	// ChargeOptionConstraints and requiredCore apply, so a repair can never
	// hand back a declaration the engine's combined-charge check rejects.
	chargeFits := func(add, remove int) bool {
		cost := d.Options[add].chargeLifeCost()
		old := int32(0)
		if remove >= 0 {
			old = d.Options[remove].chargeLifeCost()
		}
		return life < 0 || spentLife-old+cost <= life
	}
	for _, c := range choices {
		if c < 0 || c >= len(d.Options) || (have[c] && !d.Repeatable) {
			continue
		}
		o := &d.Options[c]
		if requiredObj[o.Obj] {
			if slot, ok := slotOf[o.Obj]; ok {
				old := &d.Options[out[slot]]
				if (o.Group != "" && o.Group != old.Group && groups[o.Group] >= d.GroupCapFor(o.Group)) ||
					!fits(o.Value-old.Value, o.Value2-old.Value2) || !chargeFits(c, out[slot]) {
					continue
				}
				costNew := o.chargeLifeCost()
				costOld := old.chargeLifeCost()
				spentLife += costNew - costOld
				sum += o.Value - old.Value
				sum2 += o.Value2 - old.Value2
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
		if (o.Group != "" && groups[o.Group] >= d.GroupCapFor(o.Group)) || !fits(o.Value, o.Value2) ||
			!chargeFits(c, -1) ||
			!SetPropAdmits(d.SetPropMode, setAcc, o.SetProps) {
			continue
		}
		if cost := o.chargeLifeCost(); cost > 0 {
			spentLife += cost
		}
		sum += o.Value
		sum2 += o.Value2
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
	if (d.HasBudget() && sum > d.MaxSum) || (d.HasBudget2() && sum2 > d.MaxSum2) {
		needFirst := d.HasBudget() && sum > d.MaxSum
		var neg []int
		for i := range d.Options {
			o := &d.Options[i]
			if !have[i] && ((needFirst && o.Value < 0) || (!needFirst && o.Value2 < 0)) {
				neg = append(neg, i)
			}
		}
		sort.SliceStable(neg, func(a, b int) bool {
			if needFirst {
				return d.Options[neg[a]].Value < d.Options[neg[b]].Value
			}
			return d.Options[neg[a]].Value2 < d.Options[neg[b]].Value2
		})
		for _, c := range neg {
			if ((!d.HasBudget() || sum <= d.MaxSum) && (!d.HasBudget2() || sum2 <= d.MaxSum2)) || len(out) >= d.maxChoices() {
				break
			}
			o := &d.Options[c]
			if (o.Group != "" && groups[o.Group] >= d.GroupCapFor(o.Group)) || (d.Kind == KAttackers && objTaken[o.Obj]) ||
				!chargeFits(c, -1) || (!needFirst && d.HasBudget() && sum+o.Value > d.MaxSum) ||
				(d.HasBudget2() && sum2+o.Value2 > d.MaxSum2 && sum2+o.Value2 >= sum2) {
				continue
			}
			if cost := o.chargeLifeCost(); cost > 0 {
				spentLife += cost
			}
			sum += o.Value
			sum2 += o.Value2
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
			if (o.Group != "" && groups[o.Group] >= d.GroupCapFor(o.Group)) || (d.Kind == KAttackers && objTaken[o.Obj]) ||
				!chargeFits(c, -1) || !fits(o.Value, o.Value2) {
				continue
			}
			if cost := o.chargeLifeCost(); cost > 0 {
				spentLife += cost
			}
			sum += o.Value
			sum2 += o.Value2
			out = append(out, c)
			have[c] = true
			objTaken[o.Obj] = true
			if o.Group != "" {
				groups[o.Group]++
			}
		}
	}
	// Belt: every pick above was charge-checked, so out is payable by
	// construction; re-derive through the shared pre-filter anyway so no
	// future repair path can hand back a declaration the engine's
	// combined-charge check rejects. The required core is charge-feasible by
	// the same rule, so meeting the quota is preserved -- if pruning somehow
	// dropped below it, fall back to the core, which is exactly the quota.
	if !d.ChargeOptionsFit(out) {
		if pruned := ChargeOptionConstraints(d, out, d.PayerLifeBound(), 0); d.RequiredChosen(pruned) >= d.RequiredQuota() {
			out = pruned
		} else {
			out = d.requiredCore()
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

// anyRequired reports whether some option is Required, reading each option
// in place (an Option is large, so a by-value scan copied every one).
func (d *Decision) anyRequired() bool {
	for i := range d.Options {
		if d.Options[i].Required {
			return true
		}
	}
	return false
}
