package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

// TestConditionGateTargetedCountersUseLKI pins the CR 608.2b/h counter
// look-back the condition gate shares with the amount read: a target that has
// left the battlefield since targeting is matched by
// `ConditionDefined$ Targeted | ConditionPresent$ Card.HasCounters` against
// the counters Resolve captured at the start of the chain
// (Ctx.TargetCountersLKI), not the live object the Move fold has already
// stripped. A live battlefield target keeps its live read (unchanged), and a
// departed target with no snapshot stays a resolved, unmet zero.
func TestConditionGateTargetedCountersUseLKI(t *testing.T) {
	h, ids := conditionBoard(t)
	target := h.g.Obj(ids[3])
	gate := sa(t, "DB$ PutCounter | ConditionDefined$ Targeted | ConditionPresent$ Card.HasCounters")

	// Live target with two counters: met off the live object, no snapshot.
	target.Zone = state.ZBattlefield
	target.Counters = []state.Counter{{Kind: "P1P1", N: 2}}
	ctx := &Ctx{Controller: 0, Source: ids[3], Targets: []state.Target{{Obj: ids[3]}}}
	if met, resolved := conditionMet(h, ctx, gate); !met || !resolved {
		t.Fatalf("live countered target: met=%v resolved=%v, want true true", met, resolved)
	}

	// The target leaves the battlefield (Destroy clears the live counters).
	target.Zone = state.ZGraveyard
	target.Counters = nil
	if met, resolved := conditionMet(h, ctx, gate); met || !resolved {
		t.Fatalf("departed target with no snapshot: met=%v resolved=%v, want false true", met, resolved)
	}

	// Resolution's pre-move snapshot restores the counters it had.
	ctx.Snap.TargetCounters = map[state.ObjID][]state.Counter{
		ids[3]: {{Kind: "P1P1", N: 2}},
	}
	if met, resolved := conditionMet(h, ctx, gate); !met || !resolved {
		t.Fatalf("departed target with LKI snapshot: met=%v resolved=%v, want true true", met, resolved)
	}

	// A target that is live again (a different object in game terms, but the
	// same id here) must read its LIVE counters, not the stale snapshot: the
	// look-back applies only off the battlefield.
	target.Zone = state.ZBattlefield
	target.Counters = nil
	if met, resolved := conditionMet(h, ctx, gate); met || !resolved {
		t.Fatalf("live counterless target with a stale snapshot: met=%v resolved=%v, want false true (live read wins)", met, resolved)
	}

	// The amount read (Dismantle's X:Targeted$CardCounters.ALL) shares the
	// same look-back.
	target.Zone = state.ZGraveyard
	amount := &Ctx{Controller: 0, Source: ids[3], Targets: []state.Target{{Obj: ids[3]}},
		Snap: LKISnapshots{TargetCounters: map[state.ObjID][]state.Counter{ids[3]: {{Kind: "P1P1", N: 2}, {Kind: "CHARGE", N: 1}}}}}
	if got := EvalCount(h, amount, "Targeted$CardCounters.ALL"); got != 3 {
		t.Fatalf("Targeted$CardCounters.ALL from LKI = %d, want 3", got)
	}
	// One named kind, through the same snapshot.
	amount.Snap.TargetCounters = map[state.ObjID][]state.Counter{ids[3]: {{Kind: "P1P1", N: 2}}}
	if got := EvalCount(h, amount, "Targeted$CardCounters.P1P1"); got != 2 {
		t.Fatalf("Targeted$CardCounters.P1P1 from LKI = %d, want 2", got)
	}
}
