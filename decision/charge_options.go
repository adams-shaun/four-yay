package decision

// PhyrexianLife is the life a Phyrexian pip charges when paid with life
// (CR 107.4f: two life per pip). It is the ONE home for the constant: the
// engine's combat payment (rules' combatPhyLife) and the wire-side charge
// pre-filter (ChargeOptionConstraints) both price a pip through it, so the
// bot's bound and the engine's affordability read can never disagree.
const PhyrexianLife = 2

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
// Output order is the input order: the function consumes no randomness and
// never ranges a map into the result. The engine's validateAttackers /
// validateBlockers run the stronger, board-aware combatChargeAffordable over
// the final declaration; this helper only keeps a produced answer inside the
// part of that rule the wire can express, so a bot never re-submits an answer
// the engine rejects (a livelock).
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
		if o.CostLife > 0 || o.CostPhyrexian > 0 {
			if life >= 0 && spentLife+int32(o.CostLife)+int32(o.CostPhyrexian)*PhyrexianLife > life {
				continue
			}
			spentLife += int32(o.CostLife) + int32(o.CostPhyrexian)*PhyrexianLife
		}
		spentMana += o.Value
		out = append(out, ci)
	}
	return out
}
