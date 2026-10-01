package botpolicy

import (
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// CastableObjects counts the DISTINCT castable objects a priority decision
// offers. Distinct objects, not options: one card can be offered several
// ways (an alternative cost, a kicked mode), and those are the same choice of
// card.
//
// It is the ONE definition of "a decision with a real choice between casts"
// shared by the search teacher's eligibility (searchseat.CastOptions returns
// it) and the learned seat's priority gate (seat.PolicyNetBot scores a
// priority decision only when this is >= 2), so the distribution the policy
// head was trained on and the one it is asked to answer cannot drift apart.
// Pure over the decision's options; the set is only probed, never ranged.
func CastableObjects(d *decision.Decision) int {
	seen := map[state.ObjID]bool{}
	for oi := range d.Options {
		o := &d.Options[oi]
		if o.Kind == "cast" {
			seen[o.Obj] = true
		}
	}
	return len(seen)
}
