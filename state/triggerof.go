package state

import "github.com/adams-shaun/gorge/cards"

// TriggerOf reports the T: trigger line on o's source face that minted the
// ability stack object o. One lookup, two kinds of consumer: rules'
// TargetType$ target legality needs the triggered/activated split to know
// whether a `TargetType$ Triggered` spec may target the object, and
// state.StackKindOf -- whose own consumers are rules (which delegates),
// effects' Defined$ ValidStack arm and the view's StackView.Kind -- calls it
// as its trigger-line membership test before falling through to the
// activated/delayed branches. An object that is not an ability wrapper, has
// no source, or whose source's current face lists no trigger carrying
// exactly this Effect returns false -- the caller decides what a false
// means (rules' classifier falls through to its activated/delayed branches;
// StackKindOf does too).
func TriggerOf(g *Game, o *Object) (cards.Trigger, bool) {
	if o == nil || o.Ability == nil {
		return cards.Trigger{}, false
	}
	src := g.Obj(o.Source)
	if src == nil {
		return cards.Trigger{}, false
	}
	f := src.Face()
	if f == nil {
		return cards.Trigger{}, false
	}
	for _, t := range f.Triggers {
		if t.Effect == o.Ability {
			return t, true
		}
	}
	return cards.Trigger{}, false
}
