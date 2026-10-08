package mzenc

import "github.com/adams-shaun/gorge/view"

// processStack ports StateEncoder.processStack (StateEncoder.java:413-424):
// walk the stack bottom to top. view.View.Stack keeps g.Stack's own order,
// index 0 the bottom (view/view.go:129). depth starts at 0 and increments
// before each object, so the bottom object carries Depth 1. Upstream keys each
// object's subtree by cleanString(so.toString()); gorGE uses the projected
// StackView.Name.
func (w *walker) processStack(f *Node, v *view.View) {
	w.emit(famStack)
	st := f.SubFeatures("Stack", false)
	depth := 0
	for i := range v.Stack {
		depth++
		sv := &v.Stack[i]
		so := st.SubFeatures(cleanString(sv.Name), true)
		so.AddNumericFeature("Depth", depth, false)
		w.processStackObject(so, sv, depth)
	}
}

// processStackObject ports the view-exposed subset of
// StateEncoder.processStackObject (StateEncoder.java:353-411). The view
// exposes the controller and, for a spell, the printed CardView; isController
// is emitted when the object's controller is the seat, and the cast-shape
// ability's API stands in for upstream's sa.getRule(). The target list IS
// exposed by view.StackView.Targets []TargetView, but is deliberately not yet
// wired into this skeleton; that and the family of stack-ability detail that
// view.StackView does not expose (the kicker count, the cost tags, the selected
// modes, the X value, the triggered-vs-activated distinction) emit nothing, and
// each family is recorded in w.unsupported. Task 6's register owns the honest
// reason for each.
func (w *walker) processStackObject(f *Node, sv *view.StackView, depth int) {
	if sv.Controller == w.seat {
		f.AddFeature("isController")
	}
	if sv.Card != nil && sv.Card.SpellAPI != "" {
		f.AddFeature(sv.Card.SpellAPI)
	}
	w.processStackTargets(f, sv)
	w.processStackDetail(f, sv)
}

// processStackDetail ports the cast-detail block of processStackObject (java:
// 376-404): the Kicks count, the chosen modal sub-abilities under a non-pooling
// "modes" subtree, and XValue, all with callParent false as upstream. Chosen
// modes are the sub-abilities' SVar names (gorGE's analogue of an effect's
// text). The "*_CostTag" features (java:380-387) are the one part the view does
// not carry; they are the famStackCostTags caveat.
func (w *walker) processStackDetail(f *Node, sv *view.StackView) {
	if sv.Kicks > 0 {
		f.AddNumericFeature("Kicks", int(sv.Kicks), false)
		w.emit(famStackAbilityDetail)
	}
	w.note(famStackCostTags)
	if len(sv.Modes) > 0 {
		mf := f.SubFeatures("modes", false)
		for _, m := range sv.Modes {
			mf.AddFeature(cleanString(m))
		}
		w.emit(famStackAbilityDetail)
	}
	if sv.X > 0 {
		f.AddNumericFeature("XValue", int(sv.X), false)
		w.emit(famStackAbilityDetail)
	}
}
