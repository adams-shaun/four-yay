package effects

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestConditionCollectedGatesOnEvidence pins the ConditionDefined$ Collected
// group and its CastSA>Collected spelling (task condition-collected): the group
// is the cards THIS cast collected as evidence, read through
// Host.CostMovesInWindow(events.CostMoveEvidence). The corpus gate is a paired
// pair -- the plain branch carries `ConditionPresent$ Card | ConditionCompare$
// EQ0` (run when NOTHING was collected) and the "instead" branch carries
// `ConditionPresent$ Card` (run when something was) -- so both directions are
// asserted.
func TestConditionCollectedGatesOnEvidence(t *testing.T) {
	h, ids := conditionBoard(t)

	// eq0 is the plain branch's gate: met exactly when the Collected group is
	// EMPTY (no evidence). anyEvidence is the "instead" branch's gate: met
	// exactly when the group is non-empty.
	eq0 := sa(t, "DB$ GainLife | LifeAmount$ 2 | ConditionDefined$ Collected | ConditionPresent$ Card | ConditionCompare$ EQ0")
	anyEvidence := sa(t, "DB$ GainLife | LifeAmount$ 3 | ConditionDefined$ Collected | ConditionPresent$ Card | ConditionCompare$ GE1")
	castSA := sa(t, "DB$ GainLife | LifeAmount$ 2 | ConditionDefined$ CastSA>Collected | ConditionPresent$ Card | ConditionCompare$ EQ0")

	// The precondition every assertion below rests on: each gate really is
	// classified as a SUPPORTED shape (not the fail-closed unmodelled path).
	// If Collected is left unmodelled these gates never reach the group read,
	// and the resolve walk emits an `unmodelled condition` Note instead.
	for _, c := range []struct {
		name string
		sa   *cards.SA
	}{
		{"Collected EQ0", eq0},
		{"Collected GE1", anyEvidence},
		{"CastSA>Collected EQ0", castSA},
	} {
		if detail, bad := UnmodelledCondition(c.sa); bad {
			t.Fatalf("precondition %s: gate classified unmodelled %q; the Collected shape must be supported", c.name, detail)
		}
	}

	ctx := &Ctx{Controller: 0, Source: ids[3], ResolvingObj: ids[3]}
	// No evidence: eq0 met, anyEvidence not.
	h.evidence = nil
	if met, resolved := conditionMet(h, ctx, eq0); !met || !resolved {
		t.Fatalf("Collected EQ0 with no evidence: met=%v resolved=%v, want true true", met, resolved)
	}
	if met, resolved := conditionMet(h, ctx, anyEvidence); met || !resolved {
		t.Fatalf("Collected GE1 with no evidence: met=%v resolved=%v, want false true", met, resolved)
	}
	// One evidence card: the group is non-empty, so the pair flips.
	h.evidence = []state.ObjID{ids[0]}
	if met, resolved := conditionMet(h, ctx, eq0); met || !resolved {
		t.Fatalf("Collected EQ0 with evidence: met=%v resolved=%v, want false true", met, resolved)
	}
	if met, resolved := conditionMet(h, ctx, anyEvidence); !met || !resolved {
		t.Fatalf("Collected GE1 with evidence: met=%v resolved=%v, want true true", met, resolved)
	}
	// The CastSA>Collected spelling reads the same group.
	h.evidence = nil
	if met, resolved := conditionMet(h, ctx, castSA); !met || !resolved {
		t.Fatalf("CastSA>Collected EQ0 with no evidence: met=%v resolved=%v, want true true", met, resolved)
	}
	h.evidence = []state.ObjID{ids[1]}
	if met, resolved := conditionMet(h, ctx, castSA); met || !resolved {
		t.Fatalf("CastSA>Collected EQ0 with evidence: met=%v resolved=%v, want false true", met, resolved)
	}
}

// TestConditionCollectedRunsOrSkipsBody drives the same pair through Resolve:
// with no evidence the plain branch's body runs; with evidence the plain body
// is skipped and the "instead" body runs. This is the behavioural half -- it
// fails if the Collected shape is left unmodelled (the walk would emit an
// `unmodelled condition` Note and skip BOTH branches) and it asserts that a
// body ran so a silently no-op handler cannot pass.
func TestConditionCollectedRunsOrSkipsBody(t *testing.T) {
	h, ids := conditionBoard(t)
	plain := sa(t, "DB$ GainLife | LifeAmount$ 2 | ConditionDefined$ Collected | ConditionPresent$ Card | ConditionCompare$ EQ0")
	instead := sa(t, "DB$ GainLife | LifeAmount$ 7 | ConditionDefined$ Collected | ConditionPresent$ Card | ConditionCompare$ GE1")

	// The game really holds the source the walk resolves from, so a matched
	// group member is reachable and the body can actually run.
	if h.g.Obj(ids[3]) == nil {
		t.Fatalf("precondition: source object %d is not on the game", ids[3])
	}

	lifeBodies := func(evidence []state.ObjID) (life, notes int) {
		h.log = nil
		h.evidence = evidence
		ctx := &Ctx{Controller: 0, Source: ids[3], ResolvingObj: ids[3]}
		Resolve(h, ctx, plain)
		Resolve(h, ctx, instead)
		for _, ev := range h.log {
			switch ev.Kind {
			case events.LifeChange:
				life++
			case events.Note:
				if strings.Contains(ev.Text, "unmodelled condition") {
					notes++
				}
			}
		}
		return life, notes
	}

	life, notes := lifeBodies(nil)
	if notes != 0 {
		t.Fatalf("no-evidence run emitted %d unmodelled-condition Note(s); the Collected shape must be evaluated", notes)
	}
	if life != 1 {
		t.Fatalf("no-evidence run ran %d life bodies, want 1 (the EQ0 plain branch only)", life)
	}

	life, notes = lifeBodies([]state.ObjID{ids[0]})
	if notes != 0 {
		t.Fatalf("evidence run emitted %d unmodelled-condition Note(s)", notes)
	}
	if life != 1 {
		t.Fatalf("evidence run ran %d life bodies, want 1 (the GE1 instead branch only)", life)
	}
}
