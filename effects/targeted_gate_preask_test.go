package effects

// Targeting ordering: a sub-ability that carries its OWN ValidTgts$ and a
// ConditionDefined$ Targeted gate must be RUNNABLE before its target ask has
// been posed. effects.Resolve evaluates the condition gate BEFORE
// chosenTargetsFor poses the SA's own ValidTgts$ ask, so before this ticket
// the empty group read as a RESOLVED zero and the whole sub -- target ask
// included -- was skipped. Guard Dogs' DBPrevent (a DB sub of an ACTIVATED
// ability, reached by no placement or charm ask) is the one measured corpus
// carrier; the ordinary Execute/Choices carriers have their Ctx.Targets /
// Ctx.PickedTargets populated before resolution and never hit the branch.
//
// The gate now stays UNRESOLVED exactly while (a) the effective group is
// empty, (b) the SA declares its own ValidTgts$, and (c) no ask has been
// offered or answered yet. The answered re-entry resolves for real, because
// the mid-resolution pre-ask answer (Ctx.TargetsPick, Ctx.SubPreAsk) and the
// event-backed choose are visible to the gate by then.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// targetedGateProbe registers a body API that records how many times it ran,
// so a test can tell "the sub ran" from "the sub was skipped".
func targetedGateProbe(t *testing.T) *int {
	t.Helper()
	runs := new(int)
	Register("TargetedGateProbe", func(h Host, c *Ctx, s *cards.SA) { *runs++ })
	t.Cleanup(func() { unregister("TargetedGateProbe") })
	return runs
}

// TestTargetedGateWithoutValidTgtsStaysResolvedZero pins the pre-existing
// contract this ticket must not widen: an SA with ConditionDefined$ Targeted
// and NO ValidTgts$ of its own has a genuinely definite (empty) target list,
// so the empty group is the resolved zero that skips it -- not the pre-ask
// UNRESOLVED state. The ordinary "target the parent already chose" subs
// (Stalking Leonin's exile) take this branch.
func TestTargetedGateWithoutValidTgtsStaysResolvedZero(t *testing.T) {
	h, _ := conditionBoard(t)
	gate := sa(t, "DB$ Pump | Defined$ Self | ConditionDefined$ Targeted | ConditionPresent$ Card")
	ctx := &Ctx{Controller: 0}
	if met, resolved := conditionMet(h, ctx, gate); met || !resolved {
		t.Fatalf("no-ValidTgts empty group: met=%v resolved=%v, want false true", met, resolved)
	}
}

// TestTargetedGateKeepsAPendingCharmAskResolved pins the interaction with a
// Charm/trigger gate whose target group is bound BEFORE the gate runs: the
// mode's own Ctx.Targets (charmModeTarget) and a cast-time SubPreAsk answer
// must both resolve the gate normally, never fall through to the pre-ask
// UNRESOLVED branch, so an already-targeted mode is not re-offered.
func TestTargetedGateKeepsAPendingCharmAskResolved(t *testing.T) {
	h, ids := conditionBoard(t)
	bear := ids[3]
	h.g.Obj(bear).Zone = state.ZBattlefield
	gate := sa(t, "DB$ Pump | ValidTgts$ Creature | ConditionDefined$ Targeted | ConditionPresent$ Card")

	// A charm/trigger mode whose targets are already bound (charmModeTarget):
	// resolved over the mode's group, and no ask is flagged as uncovered.
	if met, resolved := conditionMet(h, &Ctx{Controller: 0, Targets: []state.Target{{Obj: bear}}}, gate); !met || !resolved {
		t.Fatalf("bound mode target: met=%v resolved=%v, want true true", met, resolved)
	}

	// A cast-time pre-ask answer for exactly this SA (SubPreAsk by Line): the
	// gate reads it, so an empty recorded set is a resolved zero, not the
	// pre-ask UNRESOLVED state.
	subPre := &Ctx{Controller: 0, SubPreAsk: map[string][]state.Target{gate.Line: {{Obj: bear}}}}
	if met, resolved := conditionMet(h, subPre, gate); !met || !resolved {
		t.Fatalf("cast-pre-asked answer: met=%v resolved=%v, want true true", met, resolved)
	}
	subPreEmpty := &Ctx{Controller: 0, SubPreAsk: map[string][]state.Target{gate.Line: {}}}
	if met, resolved := conditionMet(h, subPreEmpty, gate); met || !resolved {
		t.Fatalf("cast-pre-asked empty answer: met=%v resolved=%v, want false true", met, resolved)
	}
}

// TestTargetedGateStillPosesTheAskWhenTheSubBodySuspends is a decision-flow
// guard: the ask posed by the pre-ask branch names the gated SA as its
// ResumeSA, so the engine's answered re-entry reaches THIS SA's gate with the
// answer bound (the seedTargetsPick/SubPreAsk paths) rather than re-posing.
func TestTargetedGateStillPosesTheAskWhenTheSubBodySuspends(t *testing.T) {
	h, ids := conditionBoard(t)
	src, bear := ids[2], ids[3]
	h.g.Obj(bear).Zone = state.ZBattlefield
	gate := sa(t, "DB$ TargetedGateProbe | ValidTgts$ Creature | ConditionDefined$ Targeted | ConditionPresent$ Card")
	_ = targetedGateProbe(t)
	Resolve(h, &Ctx{Controller: 0, Source: src}, gate)
	d := h.lastAsk
	if d == nil {
		t.Fatal("no ask posed")
	}
	if d.Kind != decision.KChoose || d.ResumeKind != "tgts" {
		t.Fatalf("ask kind/resume = %v/%q, want choose/tgts", d.Kind, d.ResumeKind)
	}
	if len(d.Options) != 1 || d.Options[0].Obj != bear {
		t.Fatalf("ask options = %+v, want the one battlefield creature %d", d.Options, bear)
	}
}
