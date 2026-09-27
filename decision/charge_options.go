package decision

// PhyrexianLife is the life a Phyrexian pip charges when paid with life
// (CR 107.4f: two life per pip). It is the ONE home for the constant: the
// engine's combat payment (rules' combatPhyLife) and the wire-side charge
// pre-filter (ChargeOptionConstraints) both price a pip through it, so the
// bot's bound and the engine's affordability read can never disagree.
const PhyrexianLife = 2

// chargeLifeCost is one option's non-mana LIFE charge under the ONE pricing
// rule: CostLife plus each Phyrexian pip at its life branch (two life,
// CR 107.4f). ChargeOptionConstraints, requiredCore, RequiredQuota and
// FitRequired all price through here, so the required-quota rule and the
// policy's charge guard cannot drift about what one option costs. It does NOT
// price a tapXType obligation: that obligation is invisible on the wire, so
// ChargeOptionConstraints drops the option outright, while the required set
// leaves it to the engine's board-aware payment (the pre-existing contract).
func (o *Option) chargeLifeCost() int32 {
	return int32(o.CostLife) + int32(o.CostPhyrexian)*PhyrexianLife
}

// ChargeOptionsFit reports whether an already-built option set's combined
// LIFE charge (CostLife plus each pip at two life) fits the decision's
// published bound (PayerLife), using the same pricing as
// ChargeOptionConstraints. It is the predicate form requiredCore, RequiredQuota
// and FitRequired's fast path read, so "can this declaration's life charge be
// paid" has exactly one home. An unpublished bound (PayerLife 0) leaves the life
// dimension inert, as ChargeOptionConstraints does. A tap obligation is not
// priced here.
func (d *Decision) ChargeOptionsFit(choices []int) bool {
	life := d.PayerLifeBound()
	spent := int32(0)
	for _, c := range choices {
		if c < 0 || c >= len(d.Options) {
			continue
		}
		if life >= 0 && spent+d.Options[c].chargeLifeCost() > life {
			return false
		}
		spent += d.Options[c].chargeLifeCost()
	}
	return true
}

// ChargeOptionConstraints applies the published-field half of the non-mana
// combat-charge answer rule shared by the KAttackers and KBlockers arms. It
// is the ONE home for the rule so the bot's policy and any other answer
// builder cannot drift from it:
//
//   - a positive Option.CostTaps is dropped: the engine's deterministic
//     tapXType obligation plan is not visible on the wire, so an answer
//     cannot prove it can meet the obligation, and the engine's board-aware
//     affordability read would reject a declaration it cannot pay;
//   - the cumulative Option.CostLife of the kept options is bounded by the
//     acting player's life total (a negative life means the total is not
//     published, so only the tap rule applies), earliest kept;
//   - each Option.CostPhyrexian pip is priced at its life branch (two life,
//     CR 107.4f) and folded into the SAME cumulative life bound as
//     Option.CostLife: the wire cannot show that the colour branch is
//     reachable, so the life branch is the conservative price, and a
//     declaration whose pips could only be paid with life must not exceed the
//     payer's life. A per-attacker pip tax (Norn's Annex) offers each pair
//     individually affordable; only the combined count overruns.
//   - when maxSum > 0 the cumulative Option.Value is bounded by it (the
//     Decision's mana budget). The KAttackers arm passes 0 and leaves the
//     mana budget to Clamp; the KBlockers arm passes the published MaxSum,
//     reproducing its historical combined pre-filter exactly.
//
// The life bound is the caller's (the published PayerLife, or the adapter's
// board total); the pricing itself is the shared chargeLifeCost, so the
// pre-filter and the required-quota rule can never price an option
// differently. Output order is the input order: the function consumes no
// randomness and never ranges a map into the result. The engine's
// validateAttackers / validateBlockers run the stronger, board-aware
// combatChargeAffordable over the final declaration; this helper only keeps a
// produced answer inside the part of that rule the wire can express, so a bot
// never re-submits an answer the engine rejects (a livelock).
func ChargeOptionConstraints(d *Decision, choices []int, life int32, maxSum int) []int {
	if len(choices) == 0 {
		return choices
	}
	out := make([]int, 0, len(choices))
	spentMana, spentLife := 0, int32(0)
	for _, ci := range choices {
		if ci < 0 || ci >= len(d.Options) {
			out = append(out, ci)
			continue
		}
		o := &d.Options[ci]
		if o.CostTaps > 0 {
			continue
		}
		if maxSum > 0 && spentMana+o.Value > maxSum {
			continue
		}
		if cost := o.chargeLifeCost(); cost > 0 {
			if life >= 0 && spentLife+cost > life {
				continue
			}
			spentLife += cost
		}
		spentMana += o.Value
		out = append(out, ci)
	}
	return out
}
