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

// TestTargetedGateLeavesAPreAskSASubRunnable drives a root SA whose SubAbility$
// carries ValidTgts$ + ConditionDefined$ Targeted through Resolve with the ask
// not yet covered: the gate must be UNRESOLVED, so the sub dispatches and
// chosenTargetsFor poses its own target ask. Once the answer is recorded
// (Ctx.PickedTargets / Ctx.TargetsPick / an answered-empty set) or the SA is
// the offered one, the gate resolves normally instead.
func TestTargetedGateLeavesAPreAskSASubRunnable(t *testing.T) {
	h, ids := conditionBoard(t)
	runs := targetedGateProbe(t)
	// A battlefield creature so the sub's ValidTgts$ Creature ask has a
	// candidate; conditionBoard leaves everything in the library. The source
	// is the Mountain so the creature is not excluded as the ask's source.
	src, bear := ids[2], ids[3]
	h.g.Obj(bear).Zone = state.ZBattlefield
	gate := sa(t, "DB$ TargetedGateProbe | ValidTgts$ Creature | ConditionDefined$ Targeted | ConditionPresent$ Card")

	// (a) No targets, no ask covered: the gate is UNRESOLVED and the sub's
	// ask is posed. The fakeHost's Ask records the decision and auto-answers
	// it, so the sub body still runs with Ctx.PickedTargets set.
	ctx := &Ctx{Controller: 0, Source: src}
	Resolve(h, ctx, gate)
	if h.lastAsk == nil || h.lastAsk.ResumeKind != "tgts" {
		t.Fatalf("pre-ask sub posed no ValidTgts$ ask: lastAsk=%+v", h.lastAsk)
	}
	if h.lastAsk.ResumeSA == nil || h.lastAsk.ResumeSA.Line != gate.Line {
		t.Fatalf("the ask's ResumeSA is not the gated sub (line mismatch)")
	}
	if *runs != 1 {
		t.Fatalf("pre-ask sub body ran %d times, want 1 (the sub must not be skipped)", *runs)
	}

	// (b) The answered re-entry resolves normally. Ctx.PickedTargets (the
	// pre-asked body channel) bearing the creature: present Card matches, the
	// gate is met and resolved.
	*runs = 0
	answered := &Ctx{Controller: 0, Source: src, PickedTargets: []state.Target{{Obj: bear}}}
	Resolve(h, answered, gate)
	if *runs != 1 {
		t.Fatalf("answered sub body ran %d times, want 1", *runs)
	}
	if h.lastAsk == nil || h.lastAsk.ResumeKind != "tgts" {
		t.Fatalf("the answered dispatch posed a fresh ask: lastAsk=%+v", h.lastAsk)
	}

	// (c) An ANSWERED-EMPTY set (a TargetMin$ 0 sub elected zero, the
	// mid-resolution transport Ctx.TargetsPick) is a resolved zero, never the
	// pre-ask state: the sub is SKIPPED and its ask is not posed.
	*runs = 0
	h.askCount, h.lastAsk = 0, nil
	empty := &Ctx{Controller: 0, Source: src, TargetsPickDone: true, TargetsPick: []state.Target{}}
	Resolve(h, empty, gate)
	if *runs != 0 {
		t.Fatalf("answered-empty sub body ran %d times, want 0 (a real zero must skip)", *runs)
	}
	if h.askCount != 0 {
		t.Fatalf("answered-empty sub posed %d asks, want 0", h.askCount)
	}

	// (d) The placement/announcement ask covered exactly this SA (Ctx.OfferedSA
	// by Line): its targets are the resolution-level Ctx.Targets, so the gate
	// resolves over them and does NOT re-pose.
	*runs = 0
	h.askCount, h.lastAsk = 0, nil
	offered := &Ctx{Controller: 0, Source: src, Targets: []state.Target{{Obj: bear}}, OfferedSA: gate}
	Resolve(h, offered, gate)
	if *runs != 1 {
		t.Fatalf("offered-covered sub body ran %d times, want 1", *runs)
	}
	if h.askCount != 0 {
		t.Fatalf("offered-covered sub posed %d asks, want 0", h.askCount)
	}
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
