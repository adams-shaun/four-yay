package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

func TestConditionDefinedParentTargetAndSacrificed(t *testing.T) {
	h, ids := conditionBoard(t)
	parent := sa(t, "DB$ Pump | ConditionDefined$ ParentTarget | ConditionPresent$ Card.Instant | ConditionCompare$ EQ1")
	ctx := &Ctx{Controller: 0, Source: ids[3], Targets: []state.Target{{Obj: ids[0]}}}
	if met, resolved := conditionMet(h, ctx, parent); !met || !resolved {
		t.Fatalf("ParentTarget instant: met=%v resolved=%v, want true true", met, resolved)
	}
	ctx.Targets = []state.Target{{Obj: ids[2]}}
	if met, resolved := conditionMet(h, ctx, parent); met || !resolved {
		t.Fatalf("ParentTarget land: met=%v resolved=%v, want false true", met, resolved)
	}

	sacrificed := sa(t, "DB$ Pump | ConditionDefined$ Sacrificed | ConditionPresent$ Card.Instant | ConditionCompare$ EQ1")
	ctx.Sacrificed = []state.SacrificedInfo{{Obj: ids[0]}}
	if met, resolved := conditionMet(h, ctx, sacrificed); !met || !resolved {
		t.Fatalf("Sacrificed instant: met=%v resolved=%v, want true true", met, resolved)
	}
	ctx.Sacrificed = []state.SacrificedInfo{{Obj: ids[2]}}
	if met, resolved := conditionMet(h, ctx, sacrificed); met || !resolved {
		t.Fatalf("Sacrificed land: met=%v resolved=%v, want false true", met, resolved)
	}
}
