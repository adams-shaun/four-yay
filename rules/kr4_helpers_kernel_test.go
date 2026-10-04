package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/state"
)

// kr4Resolve resolves sa directly under a kernel probe: an ask inside it is
// posed as e.Pending() and the answering Submit re-executes the resolution
// from the checkpoint with the recorded answers served. ctx is called afresh
// for every (re-)execution, so it must only read what was captured before.
// Any outstanding decision (the priority a fixture holds) is dropped first:
// the kernel serves an ask only with no other decision posed.
func kr4Resolve(e *Engine, ctx func() *effects.Ctx, sa *cards.SA) {
	e.pending = nil
	e.probe(func() { effects.Resolve(e, ctx(), sa) })
}

// kr4Option returns the index of the option offering obj, or fails.
func kr4Option(t *testing.T, d *decision.Decision, obj state.ObjID) int {
	t.Helper()
	for _, o := range d.Options {
		if o.Obj == obj {
			return o.Index
		}
	}
	t.Fatalf("object %d not offered: %+v", obj, d.Options)
	return -1
}

// kr4Offers reports whether d offers obj.
func kr4Offers(d *decision.Decision, obj state.ObjID) bool {
	for _, o := range d.Options {
		if o.Obj == obj {
			return true
		}
	}
	return false
}

// kr4Settle re-asks priority once a probed resolution has finished (a probe
// runs outside the engine's flow, so nothing poses the next decision): the
// state-based actions a resolution is followed by, then priority.
func kr4Settle(e *Engine) {
	if e.Pending() == nil && !e.G.Over {
		e.checkStateBased()
		if !e.G.Over {
			e.priorityRound()
		}
	}
}

// kr4PassToDecision drops the fixture's held decision, opens a priority
// round and passes priority through real Submits until a non-priority
// decision is posed or the stack empties, so stack objects resolve under the
// engine's own kernel flow. Returns the non-priority decision, or nil.
func kr4PassToDecision(t *testing.T, e *Engine, limit int) *decision.Decision {
	t.Helper()
	if d := e.Pending(); d == nil || d.Kind != decision.KPriority {
		e.pending = nil
		e.priorityRound()
	}
	for i := 0; i < limit && !e.G.Over; i++ {
		d := e.Pending()
		if d == nil {
			return nil
		}
		if d.Kind != decision.KPriority {
			return d
		}
		if len(e.G.Stack) == 0 {
			return nil
		}
		idx := -1
		for _, o := range d.Options {
			if o.Kind == "pass" {
				idx = o.Index
			}
		}
		if idx < 0 {
			t.Fatalf("priority decision with no pass option: %+v", d)
		}
		submitChoices(t, e, idx)
	}
	return e.Pending()
}
