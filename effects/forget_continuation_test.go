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
// suspends on a player answer resumes in a FRESH Ctx, so only the ask's
// own ride (Decision.ResumeForgetOther*) can carry the pre-clear candidates
// across. Each test drives a first pass to its ask, answers it by
// reconstructing the resumed Ctx the way rules' resumeResolution does
// (answers + the ask's ride fields), and asserts the pre-clear selector
// still matched through re-entry. The clear-remembered Choose event must be
// emitted exactly once per effect despite the suspensions.

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
// answers the resume arm sets plus the ask's own ForgetOtherRemembered$
// ride, exactly what rules/resolution.go's rebuild carries.
type forgetRide struct {
	d *decision.Decision
}

func (r forgetRide) ctx(base *Ctx) *Ctx {
	c := base
	c.Remembered = append([]state.Target(nil), r.d.ResumeRemembered...)
	c.Chosen = append([]state.Target(nil), r.d.ResumeChoices...)
	c.ChosenValid = r.d.ResumeChosenValid
	c.ForgetOtherSnapshot = append([]state.Target(nil), r.d.ResumeForgetOtherSnapshot...)
	c.ForgetOtherOwners = append([]state.PlayerID(nil), r.d.ResumeForgetOtherOwners...)
	c.ForgetOtherReady = r.d.ResumeForgetOtherReady
	c.ForgetOtherCleared = r.d.ResumeForgetOtherCleared
	return c
}
