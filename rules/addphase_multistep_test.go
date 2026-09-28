package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// The DB$ AddPhase turn-walk tests for the shapes effects/addphase.go's
// multi-step resolution mints (ticket ap1: the multi-step-set half of the
// row, plus the two deliberate consumer behaviours the row called untested).
// effects/addphase_multistep_test.go owns the parse side; these drive the
// real step machinery through the spliced ranges and replay-check the log.
//
// Every fixture drives to turn 3 seat 0 (a turn-1 creature is summoning
// sick, and a combat with no eligible attacker poses no attackers ask --
// CR 508.1's forced empty declaration), so the ordinary combats these tests
// resume into pose a real attackers ask to stop at.

// TestExtraPhaseMultiStepRangeWalksItsWholeRange: a grant whose ExtraPhase$
// value named a multi-step phase ("Upkeep,Draw" -- the RANGEEND rider) walks
// its WHOLE range: the extra upkeep and the extra draw both run, the grant
// completes when the walk leaves the range end (Draw), and the walk resumes
// at the splice point's natural successor (Main1+1, the ordinary combat).
func TestExtraPhaseMultiStepRangeWalksItsWholeRange(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := addPhaseEngine(t, reg, []*cards.Card{card(t, bearSrc)}, nil)
	moveByName(t, e, 0, "Bear", state.ZBattlefield)
	driveToTurn(t, e, 3, 0)
	// The grant a multi-step ExtraPhase$ value mints: extra Upkeep..Draw
	// spliced after Main1. The RANGEEND rider is load-bearing: without it
	// the fold derives the entry's own range (Upkeep only) and the extra
	// draw never runs.
	e.emit(events.Event{Kind: events.ExtraPhase, Player: 0, Amount: 1,
		Step: state.StepMain1, IDs: []state.ObjID{state.ObjID(state.StepUpkeep)},
		Text: events.EncodeExtraPhaseRiders(events.ExtraPhaseRiders{
			HasRangeEnd: true, RangeEnd: state.StepDraw})})
	if len(e.G.ExtraPhases) != 1 {
		t.Fatalf("ExtraPhases = %+v, want the one grant", e.G.ExtraPhases)
	}
	if ep := e.G.ExtraPhases[0]; ep.RangeEnd != state.StepDraw {
		t.Fatalf("fold RangeEnd = %s, want Draw (the rider must override the entry's default)", ep.RangeEnd)
	}
	upkeeps := countSteps(e, state.StepUpkeep)
	draws := countSteps(e, state.StepDraw)
	begins := countSteps(e, state.StepBeginCombat)
	// Leave Main1: the extra upkeep and extra draw run, then the walk
	// resumes at the ordinary combat (the splice point's natural successor).
	passToKind(t, e, decision.KAttackers)
	if got := countSteps(e, state.StepUpkeep) - upkeeps; got != 1 {
		t.Fatalf("%d extra upkeep steps ran, want 1", got)
	}
	if got := countSteps(e, state.StepDraw) - draws; got != 1 {
		t.Fatalf("%d extra draw steps ran, want 1 (the range must reach its Draw end)", got)
	}
	if got := countSteps(e, state.StepBeginCombat) - begins; got != 1 {
		t.Fatalf("%d combats followed the extra range, want 1 (the ordinary one)", got)
	}
	if len(e.G.ExtraPhases) != 0 {
		t.Fatalf("ExtraPhases not drained at the range end: %+v", e.G.ExtraPhases)
	}
	replayCheck(t, e, cfg)
}

// TestExtraPhaseSpliceMidRangeContinuesTheOuterRange pins the deliberate
// mid-range behaviour the row called untested: a grant spliced INSIDE
// another active extra phase's range (here an extra Upkeep spliced after
// Declare Blockers, while an extra Combat is running) consumes and runs at
// its own boundary, its completion resumes at ITS splice point's natural
// successor (Declare Blockers+1, Combat Damage -- the outer range's own
// continuation here, not the upkeep's natural Draw), and after the inner
// extra phase completes the OUTER extra phase still completes at its range
// end and the walk resumes at the outer splice point's natural successor
// (the ordinary combat).
//
// The splice point is deliberately Declare Blockers and not Combat Damage:
// the combat-damage pass jumps to the end-of-combat step OUTSIDE the
// consumer site (rules/turn.go's extraPhaseBoundary doc), so a grant whose
// splice point is combat damage is never seen -- the same blind spot the
// row's design note records for declare-attackers and combat-damage splices.
func TestExtraPhaseSpliceMidRangeContinuesTheOuterRange(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := addPhaseEngine(t, reg,
		[]*cards.Card{card(t, bearSrc), card(t, bearSrc)}, nil)
	bear1 := moveByName(t, e, 0, "Bear", state.ZBattlefield)
	moveByName(t, e, 0, "Bear", state.ZBattlefield) // bear2: the ordinary combat's ask
	driveToTurn(t, e, 3, 0)
	// A: an extra combat spliced after Main1. B: an extra upkeep spliced
	// after Combat Damage -- a point inside A's range.
	e.emit(events.Event{Kind: events.ExtraPhase, Player: 0, Amount: 1,
		Step: state.StepMain1, IDs: []state.ObjID{state.ObjID(state.StepBeginCombat)}})
	e.emit(events.Event{Kind: events.ExtraPhase, Player: 0, Amount: 1,
		Step: state.StepDeclareBlockers, IDs: []state.ObjID{state.ObjID(state.StepUpkeep)}})
	upkeeps := countSteps(e, state.StepUpkeep)
	begins := countSteps(e, state.StepBeginCombat)
	ends := countSteps(e, state.StepEndCombat)
	draws := countSteps(e, state.StepDraw)
	passToKind(t, e, decision.KAttackers)
	submitAttackers(t, e, bear1) // submits and crosses the CR 508.2 window
	driveToStep(t, e, 3, 0, state.StepDeclareAttackers)
	// The mid-range splice ran the extra upkeep INSIDE the extra combat, and
	// its completion resumed at Combat Damage (Declare Blockers' natural
	// successor) -- not at the upkeep step's own natural successor (Draw).
	if got := countSteps(e, state.StepUpkeep) - upkeeps; got != 1 {
		t.Fatalf("%d extra upkeep steps ran, want 1 (the mid-range splice)", got)
	}
	if got := countSteps(e, state.StepDraw) - draws; got != 0 {
		t.Fatalf("%d draw steps ran after the inner extra upkeep, want 0 (the resume is the outer range's continuation)", got)
	}
	if got := countSteps(e, state.StepEndCombat) - ends; got != 1 {
		t.Fatalf("%d end-of-combat steps ran, want 1 (the outer extra combat's)", got)
	}
	if got := countSteps(e, state.StepBeginCombat) - begins; got != 2 {
		t.Fatalf("%d combats ran since the grants, want 2 (the extra combat and the ordinary one)", got)
	}
	if len(e.G.ExtraPhases) != 0 {
		t.Fatalf("ExtraPhases not drained: %+v", e.G.ExtraPhases)
	}
	if e.G.Step != state.StepDeclareAttackers {
		t.Fatalf("step %s, want the ordinary combat's attackers ask (the outer range completed and resumed)", e.G.Step)
	}
	replayCheck(t, e, cfg)
}

// TestExtraPhaseMultiCompletionTakesTheLastResume pins the deliberate
// multi-completion behaviour the row named: when two consumed grants
// complete at the same leaving step (the inner extra combat B, granted and
// consumed inside the outer extra combat A, ends where A's own range ends),
// the walk continues at the LAST completed grant's resume point -- the
// innermost one's (B's explicit FollowedBy$, the end step), not the outer
// grant's default (Main1+1, which would re-enter a combat).
func TestExtraPhaseMultiCompletionTakesTheLastResume(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := addPhaseEngine(t, reg,
		[]*cards.Card{card(t, bearSrc), card(t, bearSrc)}, nil)
	bear1 := moveByName(t, e, 0, "Bear", state.ZBattlefield)
	bear2 := moveByName(t, e, 0, "Bear", state.ZBattlefield)
	driveToTurn(t, e, 3, 0)
	// A: an extra combat after Main1 (default resume: Main1+1, the ordinary
	// combat). B: an extra combat spliced after Declare Attackers -- inside
	// A's range -- with an explicit FollowedBy$ (the end step).
	e.emit(events.Event{Kind: events.ExtraPhase, Player: 0, Amount: 1,
		Step: state.StepMain1, IDs: []state.ObjID{state.ObjID(state.StepBeginCombat)}})
	e.emit(events.Event{Kind: events.ExtraPhase, Player: 0, Amount: 1,
		Step: state.StepDeclareAttackers,
		IDs:  []state.ObjID{state.ObjID(state.StepBeginCombat), state.ObjID(state.StepEnd)}})
	begins := countSteps(e, state.StepBeginCombat)
	ends := countSteps(e, state.StepEndCombat)
	passToKind(t, e, decision.KAttackers)
	submitAttackersOnly(t, e, bear1)
	// Leaving Declare Attackers consumes B: the inner extra combat's own
	// attackers ask comes up.
	passToKind(t, e, decision.KAttackers)
	submitAttackersOnly(t, e, bear2)
	// B's combat resolves; at its end-of-combat step BOTH consumed grants
	// complete (B's range and A's range both end there), and the walk takes
	// B's resume point -- the end step -- rather than A's (which would
	// re-enter a combat: a third BeginCombat would show here).
	driveToStep(t, e, 3, 0, state.StepEnd)
	if got := countSteps(e, state.StepBeginCombat) - begins; got != 2 {
		t.Fatalf("%d combats ran since the grants, want 2 (A's and B's)", got)
	}
	if got := countSteps(e, state.StepEndCombat) - ends; got != 1 {
		t.Fatalf("%d end-of-combat steps ran, want 1 (B's)", got)
	}
	if len(e.G.ExtraPhases) != 0 {
		t.Fatalf("ExtraPhases not drained: %+v", e.G.ExtraPhases)
	}
	if e.G.Step != state.StepEnd {
		t.Fatalf("step %s, want the end step (the LAST completed grant's resume)", e.G.Step)
	}
	replayCheck(t, e, cfg)
}
