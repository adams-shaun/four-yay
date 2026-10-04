package effects

import (
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The ForgetOtherRemembered$ continuation leaves (ticket: preserve
// IsRemembered selectors across a suspended ForgetOtherRemembered). The
// synchronous leaves are forget_other_remembered_test.go's; these pin the
// ASKED passes: a walk whose first pass clears the remembered set and then
// asks a player, answered in a FRESH Ctx, so only the ask's own ride
// (Decision.ResumeForgetOther*) can carry the pre-clear candidates across.
// Each test drives a first pass to its ask, answers it by reconstructing a
// fresh Ctx from the answers plus the ask's ride fields, and asserts the
// pre-clear selector still matched on the answered pass. The
// clear-remembered Choose event must be emitted exactly once per effect
// despite the asks.

// clearRememberedCount counts the source's clear-remembered Choose events
// in the fixture host's log.
func clearRememberedCount(h *fakeHost, src state.ObjID) int {
	n := 0
	for _, e := range h.log {
		if e.Kind == events.Choose && e.Counter == "clear-remembered" && e.Obj == src {
			n++
		}
	}
	return n
}

// forgetRide is the shared fresh-Ctx reconstruction of one answered ask: the
// answers plus the ask's own ForgetOtherRemembered$ ride.
type forgetRide struct {
	d *decision.Decision
}
