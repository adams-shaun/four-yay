// resolution_point.go holds the resume point: the small record a rules-side
// payment window carries to re-enter a resolution's body once the window has
// settled (resumeResolution, rules/resolution.go).
package rules

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// resumePoint names the body to re-enter: which stack object's resolution,
// the ability to run and the kind of window that settled before it. Plain
// data (the *cards.SA is shared immutable card data).
type resumePoint struct {
	// kind is "optional" (an optional trigger's yes), "effect_paid" (a
	// triggered Cost$ window paid) or "copy_targets" (a copy's new-target
	// election).
	kind string
	obj  state.ObjID
	sa   *cards.SA
	// tapPaidX is the count the triggered-cost window's dynamic tapXType<X/
	// Spec> election paid; winPaidX is the X the window's X fold announced or
	// fixed. Each seeds Ctx.X on the re-entry (the trigger object was never
	// paid an X). Zero elsewhere.
	tapPaidX, winPaidX int32
}
