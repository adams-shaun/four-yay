package pay

// ManaStatic is the payment census's reads of a mana ability's own
// text (windowManaUnits, paymentPlanManaUnits, the planner's alternatives):
// pure functions of its Params and compiled cost.
type ManaStatic struct {
	Produced      string // TrimSpace(Produced$)
	Counts        [6]int32
	Any           bool  // cards.ProducedCounts(Produced$)
	Amount        int32 // availableAmount
	RestrictValid bool  // a non-blank RestrictValid$
	FreeCost      bool  // manaFreeCost(cost)
	TapOnly       bool  // paymentPlanTapOnlyCost(cost)
	Tap, Untap    bool  // cost.Tap, cost.Untap
}
