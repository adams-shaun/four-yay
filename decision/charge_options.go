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
// dimension inert, as ChargeOptionConstraints does. The declaration's
// tapXType pool (ChargeTapPoolFit) is part of the same predicate: a set that
// exhausts a published tap pool is not fit, so the repair never restores one.
func (d *Decision) ChargeOptionsFit(choices []int) bool {
	if !d.ChargeTapPoolFit(choices) {
		return false
	}
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

// ChargeTapPoolFit reports whether the chosen option set leaves the
// declaration's tapXType obligation a payable pool under the published
// ChargeTapPool. It is the ONE home for the declaration-dependent tap rule:
// Decision.Validate rejects an answer it refuses, ChargeOptionConstraints
// drops a pick that would violate it, ChargeOptionsFit folds it into the
// repair predicate, and the engine's board-aware validateAttackers calls it
// before its own combatChargeAffordable read. An unpublished pool
// (ChargeTapPool 0) leaves the rule inert, so every tap-free declaration is
// byte-identically unaffected.
//
// The arithmetic is exact for the shape the engine publishes: every offered
// tap obligation shares one candidate spec (the engine refuses to publish a
// pool otherwise), so the candidates the declaration consumes are exactly the
// selected permanents that are pool members (Option.TapPoolCost, 0 or 1) and
// the obligations it owes are the chosen options' CostTaps. A declaration
// leaves a payable pool when pool - consumed >= owed.
func (d *Decision) ChargeTapPoolFit(choices []int) bool {
	if d.ChargeTapPool <= 0 {
		return true
	}
	consumed, owed := 0, 0
	for _, c := range choices {
		if c < 0 || c >= len(d.Options) {
			continue
		}
		consumed += d.Options[c].TapPoolCost
		owed += d.Options[c].CostTaps
	}
	return d.ChargeTapPool-consumed >= owed
}

// ChargeOptionConstraints applies the published-field half of the non-mana
// combat-charge answer rule shared by the KAttackers and KBlockers arms. It
// is the ONE home for the rule so the bot's policy and any other answer
// builder cannot drift from it:
//
//   - a positive Option.CostTaps is dropped unless the decision published
//     the tap-candidate pool it draws from (Decision.ChargeTapPool) and the
//     running declaration still leaves it payable after this option's own
//     Option.TapPoolCost is consumed; the shared ChargeTapPoolFit states
//     the same rule, so the filter and Decision.Validate cannot disagree.
//     With no published pool the pick is dropped outright, the historical
//     conservative direction (the wire cannot prove the obligation);
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
	spentPool, spentTaps := 0, 0
	for _, ci := range choices {
		if ci < 0 || ci >= len(d.Options) {
			out = append(out, ci)
			continue
		}
		o := &d.Options[ci]
		// The declaration-dependent tap rule. With a published pool, keep the
		// pick only while the pool the declaration leaves still covers the
		// obligations it owes (the shared ChargeTapPoolFit arithmetic, read
		// incrementally here). Without a published pool, any tap participation
		// is unverifiable and dropped -- the same conservative direction the
		// old blanket rule took, kept for a hand-built decision that publishes
		// no pool.
		if d.ChargeTapPool <= 0 {
			if o.CostTaps > 0 || o.TapPoolCost > 0 {
				continue
			}
		} else if d.ChargeTapPool-(spentPool+o.TapPoolCost) < spentTaps+o.CostTaps {
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
		spentPool += o.TapPoolCost
		spentTaps += o.CostTaps
		out = append(out, ci)
	}
	return out
}
