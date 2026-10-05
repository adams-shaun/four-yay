package effects

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/events"
)

// TestUnresolvedConditionFailsClosed catches gates whose keys are supported
// but whose predicate shape cannot be evaluated (as distinct from a census-listed key).
func TestUnresolvedConditionFailsClosed(t *testing.T) {
	s := sa(t, "DB$ GainLife | LifeAmount$ 2 | ConditionPresent$ Card.UnknownConditionPredicate")
	if _, ok := s.Params["ConditionPresent"]; !ok {
		t.Fatal("precondition: the parsed SA must carry ConditionPresent$")
	}
	h := newHost(t, 2)
	ctx := &Ctx{Controller: 0}
	if _, resolved := conditionMet(h, ctx, s); resolved {
		t.Fatal("precondition: unknown predicate must exercise the unresolved-gate path")
	}

	Resolve(h, ctx, s)
	var note, bodyRan bool
	for _, ev := range h.log {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "unmodelled condition") {
			note = true
		}
		if ev.Kind == events.LifeChange {
			bodyRan = true
		}
		if ev.Kind == events.Note && strings.Contains(ev.Text, "unimplemented API") {
			t.Fatalf("condition test did not reach a registered effect: %q", ev.Text)
		}
	}
	if !note {
		t.Fatalf("unresolved condition emitted no replay-visible Note; log=%+v", h.log)
	}
	if bodyRan {
		t.Fatalf("body ran despite unresolved condition; log=%+v", h.log)
	}
}
