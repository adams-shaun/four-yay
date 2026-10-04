package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

// A remembered object may also be a target of the resolving chain. The
// target's counter look-back must not turn unrelated Remembered reads into
// battlefield LKI reads after that object has moved.
func TestTargetCounterLKIOnlyAppliesToTargetedReads(t *testing.T) {
	h, ids := conditionBoard(t)
	id := ids[3]
	o := h.g.Obj(id)
	o.Zone = state.ZGraveyard
	o.Counters = nil
	c := &Ctx{Controller: 0, Targets: []state.Target{{Obj: id}},
		Remembered: []state.Target{{Obj: id}},
		Snap:       LKISnapshots{TargetCounters: map[state.ObjID][]state.Counter{id: {{Kind: "P1P1", N: 2}}}}}
	if o.Zone != state.ZGraveyard || o.Counter("P1P1") != 0 || len(c.Snap.TargetCounters[id]) != 1 || c.Snap.TargetCounters[id][0].N != 2 {
		t.Fatalf("precondition: departed target live=%+v snapshot=%+v, want live 0 vs LKI 2", o, c.Snap.TargetCounters[id])
	}
	for _, tc := range []struct {
		group string
		want  bool
	}{
		{"Targeted", true},
		{"Remembered", false},
	} {
		gate := sa(t, "DB$ PutCounter | ConditionDefined$ "+tc.group+" | ConditionPresent$ Card.HasCounters")
		if met, resolved := conditionMet(h, c, gate); !resolved || met != tc.want {
			t.Errorf("%s gate: met=%v resolved=%v, want %v true", tc.group, met, resolved, tc.want)
		}
	}
	if got := EvalCount(h, c, "Targeted$CardCounters.ALL"); got != 2 {
		t.Errorf("targeted counter count = %d, want 2", got)
	}
	if got := EvalCount(h, c, "Remembered$CardCounters.ALL"); got != 0 {
		t.Errorf("remembered live counter count = %d, want 0", got)
	}
}
