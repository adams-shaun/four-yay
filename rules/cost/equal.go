package cost

import "slices"

// costFieldCount is the number of fields Cost has; TestCostEqualCoversEveryField
// (equal_test.go) fails when Cost grows without Equal growing with it.
const costFieldCount = 43

// sliceEqual is reflect.DeepEqual's slice semantics for comparable elements:
// a nil slice equals only a nil slice, and two non-nil slices are equal when
// their elements are.
func sliceEqual[T comparable](a, b []T) bool {
	if (a == nil) != (b == nil) {
		return false
	}
	return slices.Equal(a, b)
}

// Equal is reflect.DeepEqual(*c, *o) over Cost, typed: every field, with
// nil and non-nil empty slices distinct (DeepEqual's strictness).
func (c *Cost) Equal(o *Cost) bool {
	return true &&
		c.Colored == o.Colored &&
		c.Generic == o.Generic &&
		c.Life == o.Life &&
		c.X == o.X &&
		c.XMin == o.XMin &&
		sliceEqual(c.Hybrid, o.Hybrid) &&
		sliceEqual(c.Phyrexian, o.Phyrexian) &&
		sliceEqual(c.Twobrid, o.Twobrid) &&
		sliceEqual(c.HybridPhyrexian, o.HybridPhyrexian) &&
		c.Snow == o.Snow &&
		c.Waterbend == o.Waterbend &&
		c.WaterbendX == o.WaterbendX &&
		c.Tap == o.Tap &&
		c.Untap == o.Untap &&
		sliceEqual(c.Sac, o.Sac) &&
		sliceEqual(c.Discard, o.Discard) &&
		sliceEqual(c.SubCounter, o.SubCounter) &&
		sliceEqual(c.AddCounter, o.AddCounter) &&
		sliceEqual(c.Exile, o.Exile) &&
		sliceEqual(c.ExileFromTop, o.ExileFromTop) &&
		sliceEqual(c.Reveal, o.Reveal) &&
		sliceEqual(c.RevealOrChoose, o.RevealOrChoose) &&
		sliceEqual(c.RevealChosen, o.RevealChosen) &&
		sliceEqual(c.Behold, o.Behold) &&
		sliceEqual(c.TapPermanent, o.TapPermanent) &&
		sliceEqual(c.UntapPermanent, o.UntapPermanent) &&
		sliceEqual(c.Blight, o.Blight) &&
		sliceEqual(c.Exert, o.Exert) &&
		c.Forage == o.Forage &&
		sliceEqual(c.Draw, o.Draw) &&
		sliceEqual(c.Energy, o.Energy) &&
		sliceEqual(c.LifeX, o.LifeX) &&
		c.LifeHalfUp == o.LifeHalfUp &&
		sliceEqual(c.DamageYou, o.DamageYou) &&
		sliceEqual(c.GainLife, o.GainLife) &&
		sliceEqual(c.Return, o.Return) &&
		sliceEqual(c.PutToLib, o.PutToLib) &&
		sliceEqual(c.MoveToGrave, o.MoveToGrave) &&
		sliceEqual(c.Mill, o.Mill) &&
		sliceEqual(c.Evidence, o.Evidence) &&
		sliceEqual(c.RollDice, o.RollDice) &&
		sliceEqual(c.Withheld, o.Withheld) &&
		sliceEqual(c.Unknown, o.Unknown)
}

// IsZero is reflect.ValueOf(*c).IsZero() over Cost, typed: every scalar is
// zero and every slice is nil (a non-nil empty slice is NOT zero).
func (c *Cost) IsZero() bool {
	var z Cost
	return c.Equal(&z)
}
