package botpolicy

// The cast-timing window features (task autopay-cast-timing-features): the
// four step facts the auto-pay adapter made reachable at decide time --
// InstantOwnPreMain, InstantOwnCombat, InstantOppTurn, InstantOppEnd. Under
// auto-pay a planned instant-speed cast is offered at EVERY priority window,
// so these weights (paired with CastThreshold) are the hold/cast boundaries
// per window. Every one is weight 0 in the default profile, so the default
// bot is byte-identical; each test below asserts the default pick first so
// the flip is attributable to the weight alone.

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// timingBoard is a one-instant board at a given step and turn ownership: a
// CMC 2 instant (score 2 under the default profile) and a pass option. The
// instant is deliberately not Castable so it earns no C7 reserve bonus and
// the score the threshold reads is exactly its base worth plus any window
// term.
func timingBoard(step state.Step, myTurn bool) Board {
	return Board{
		Step:   step,
		MyTurn: myTurn,
		Cards:  TableOf(map[state.ObjID]Card{1: {CMC: 2, InstantSpeed: true}}),
	}
}

func timingDecision() *decision.Decision {
	return &decision.Decision{Player: 0, Kind: decision.KPriority, Min: 1, Max: 1,
		Options: []decision.Option{castSpell(0, 1), {Index: 1, Kind: "pass"}}}
}

// TestCastWeightInstantOwnPreMain: the feature fires only for an
// instant-speed card on the seat's OWN turn before main 1 (upkeep, draw,
// untap). A negative weight holds the cast there; a non-instant card in the
// same window is untouched.
func TestCastWeightInstantOwnPreMain(t *testing.T) {
	d := timingDecision()
	for _, step := range []state.Step{state.StepUntap, state.StepUpkeep, state.StepDraw} {
		b := timingBoard(step, true)
		if got := withTuned(b, func(w *CastWeights) { w.InstantOwnPreMain = -1000; w.CastThreshold = 0 }).chooseCast(d); got != -1 {
			t.Fatalf("step %s: InstantOwnPreMain=-1000 with threshold 0 = option %d, want -1 (held)", step, got)
		}
	}
	// Not in main1 (the boundary): the same board in main 1 casts.
	if got := withTuned(timingBoard(state.StepMain1, true), func(w *CastWeights) { w.InstantOwnPreMain = -1000; w.CastThreshold = 0 }).chooseCast(d); got != 0 {
		t.Fatalf("main1: InstantOwnPreMain=-1000 with threshold 0 = option %d, want 0 (cast) — the feature is pre-main-gated", got)
	}
	// Not another seat's turn.
	if got := withTuned(timingBoard(state.StepUpkeep, false), func(w *CastWeights) { w.InstantOwnPreMain = -1000; w.CastThreshold = 0 }).chooseCast(d); got != 0 {
		t.Fatalf("opp turn upkeep: InstantOwnPreMain=-1000 = option %d, want 0 — the feature is MyTurn-gated", got)
	}
	// A non-instant card in the window earns nothing: the negative weight
	// cannot reach it, so it is still cast.
	plain := timingBoard(state.StepUpkeep, true)
	plain.Cards = TableOf(map[state.ObjID]Card{1: {CMC: 2}})
	if got := withTuned(plain, func(w *CastWeights) { w.InstantOwnPreMain = -1000; w.CastThreshold = 0 }).chooseCast(d); got != 0 {
		t.Fatalf("upkeep non-instant: InstantOwnPreMain=-1000 = option %d, want 0 — the term is InstantSpeed-gated", got)
	}
}

// TestCastWeightInstantOwnCombat: the feature fires only in the seat's own
// combat steps.
func TestCastWeightInstantOwnCombat(t *testing.T) {
	d := timingDecision()
	for _, step := range []state.Step{state.StepBeginCombat, state.StepDeclareAttackers,
		state.StepDeclareBlockers, state.StepCombatDamage, state.StepEndCombat} {
		b := timingBoard(step, true)
		if got := withTuned(b, func(w *CastWeights) { w.InstantOwnCombat = -1000; w.CastThreshold = 0 }).chooseCast(d); got != -1 {
			t.Fatalf("step %s: InstantOwnCombat=-1000 with threshold 0 = option %d, want -1 (held)", step, got)
		}
	}
	// main2 is not a combat step, and neither is the opponent's combat.
	if got := withTuned(timingBoard(state.StepMain2, true), func(w *CastWeights) { w.InstantOwnCombat = -1000; w.CastThreshold = 0 }).chooseCast(d); got != 0 {
		t.Fatalf("main2: InstantOwnCombat=-1000 = option %d, want 0 — main2 is not a combat step", got)
	}
	if got := withTuned(timingBoard(state.StepDeclareAttackers, false), func(w *CastWeights) { w.InstantOwnCombat = -1000; w.CastThreshold = 0 }).chooseCast(d); got != 0 {
		t.Fatalf("opp combat: InstantOwnCombat=-1000 = option %d, want 0 — the feature is MyTurn-gated", got)
	}
}

// TestCastWeightInstantOppTurn: the feature fires for an instant-speed card
// in ANY step of another seat's turn, own-turn windows untouched.
func TestCastWeightInstantOppTurn(t *testing.T) {
	d := timingDecision()
	for _, step := range []state.Step{state.StepUpkeep, state.StepMain1, state.StepDeclareBlockers, state.StepMain2, state.StepEnd} {
		b := timingBoard(step, false)
		if got := withTuned(b, func(w *CastWeights) { w.InstantOppTurn = -1000; w.CastThreshold = 0 }).chooseCast(d); got != -1 {
			t.Fatalf("opp step %s: InstantOppTurn=-1000 with threshold 0 = option %d, want -1 (held)", step, got)
		}
	}
	// The seat's own turn is not "opp turn".
	if got := withTuned(timingBoard(state.StepMain1, true), func(w *CastWeights) { w.InstantOppTurn = -1000; w.CastThreshold = 0 }).chooseCast(d); got != 0 {
		t.Fatalf("own main1: InstantOppTurn=-1000 = option %d, want 0 — the feature is !MyTurn-gated", got)
	}
}

// TestCastWeightInstantOppEnd: the feature fires for an instant-speed card in
// another seat's END step only, and is a subset of InstantOppTurn (a board
// can price both at once, and the opp-end weight reaches no other step).
func TestCastWeightInstantOppEnd(t *testing.T) {
	d := timingDecision()
	b := timingBoard(state.StepEnd, false)
	if got := withTuned(b, func(w *CastWeights) { w.InstantOppEnd = -1000; w.CastThreshold = 0 }).chooseCast(d); got != -1 {
		t.Fatalf("opp end: InstantOppEnd=-1000 with threshold 0 = option %d, want -1 (held)", got)
	}
	// The seat's own end step is not the opponent's end step.
	if got := withTuned(timingBoard(state.StepEnd, true), func(w *CastWeights) { w.InstantOppEnd = -1000; w.CastThreshold = 0 }).chooseCast(d); got != 0 {
		t.Fatalf("own end: InstantOppEnd=-1000 = option %d, want 0 — the feature is !MyTurn-gated", got)
	}
	// Another step of the opponent's turn does not earn the end term.
	if got := withTuned(timingBoard(state.StepMain2, false), func(w *CastWeights) { w.InstantOppEnd = -1000; w.CastThreshold = 0 }).chooseCast(d); got != 0 {
		t.Fatalf("opp main2: InstantOppEnd=-1000 = option %d, want 0 — the feature is end-step-gated", got)
	}
	// Both opp-turn weights reach the end step together.
	both := withTuned(b, func(w *CastWeights) { w.InstantOppTurn = -1; w.InstantOppEnd = -1000; w.CastThreshold = 0 })
	if got := both.chooseCast(d); got != -1 {
		t.Fatalf("opp end with both opp weights = option %d, want -1 — the terms stack", got)
	}
}

// TestCastWeightTimingDefaultIsZero pins the byte-identity half of the brief:
// on every new-feature window a Board carrying DefaultCastWeights (all four
// weights 0) picks exactly what the literal pre-refactor rule picks.
func TestCastWeightTimingDefaultIsZero(t *testing.T) {
	d := timingDecision()
	for _, tc := range []struct {
		step   state.Step
		myTurn bool
	}{
		{state.StepUpkeep, true}, {state.StepDraw, true},
		{state.StepBeginCombat, true}, {state.StepDeclareBlockers, true},
		{state.StepUpkeep, false}, {state.StepEnd, false},
	} {
		b := timingBoard(tc.step, tc.myTurn)
		b.Cast = DefaultCastWeights
		if got := b.chooseCast(d); got != legacyChooseCast(b, d) {
			t.Fatalf("step %s myTurn=%v: default pick = option %d, want the pre-refactor pick", tc.step, tc.myTurn, got)
		}
	}
}

// TestParseCastProfileAcceptsTimingWindows proves the strict profile loader
// (profile.go's DisallowUnknownFields) accepts the four new weight names and
// that they reach the scorer: a JSON profile holding InstantOwnPreMain holds
// the instant in own upkeep (the same board the hand-built test above uses),
// so a tuner can write one of these profiles by hand and have it play.
func TestParseCastProfileAcceptsTimingWindows(t *testing.T) {
	doc := `{"version":1,"cast":{"InstantOwnPreMain":-1000,"CastThreshold":0}}`
	w, err := ParseCastProfile([]byte(doc))
	if err != nil {
		t.Fatalf("ParseCastProfile(timing profile): %v", err)
	}
	if w.InstantOwnPreMain != -1000 || w.CastThreshold != 0 {
		t.Fatalf("parsed timing profile = %+v, want InstantOwnPreMain -1000 and CastThreshold 0", w)
	}
	b := timingBoard(state.StepUpkeep, true)
	b.Cast = w
	if got := b.chooseCast(timingDecision()); got != -1 {
		t.Fatalf("parsed InstantOwnPreMain=-1000 profile in own upkeep = option %d, want -1 (held)", got)
	}
}
