package botpolicy

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// A KTarget decision carrying a cumulative MaxTotalTargetPower$ budget
// (Reunion of the House: "any number of creature cards with total power 10
// or less") must be answered by the deterministic bot with a set whose
// Value sum fits the budget. The targeting heuristic is price-blind -- it
// fires its full legal width (Min 0 picks up to Max) -- so without the
// Clamp repair its answer would bust Decision.MaxSum, Decision.Validate
// would reject it before consuming the decision, and the bot would
// re-derive the same rejected answer forever. Clamp's FitRequired repair is
// the ONE home of the budget rule for this kind, so this test runs the
// bot's own answer through Validate on the board where the constraint
// binds.
func TestTargetBudgetClampKeepsTheBotAnswerValid(t *testing.T) {
	b := Board{Cards: TableOf(map[state.ObjID]Card{})}
	d := decision.Decision{
		Player: 0,
		Kind:   decision.KTarget,
		Min:    0,
		Max:    4,
		MaxSum: 10,
		Options: []decision.Option{
			{Index: 0, Kind: "permanent", Obj: state.ObjID(1), Value: 11}, // over the cap alone
			{Index: 1, Kind: "permanent", Obj: state.ObjID(2), Value: 6},
			{Index: 2, Kind: "permanent", Obj: state.ObjID(3), Value: 4},
			{Index: 3, Kind: "permanent", Obj: state.ObjID(4), Value: 3},
		},
	}
	in := Decide(b, &d, rng(1))
	if err := d.Validate(in); err != nil {
		t.Fatalf("bot answer %v failed Validate: %v", in.Choices, err)
	}
	sum := 0
	for _, c := range in.Choices {
		sum += d.Options[c].Value
	}
	if sum > 10 {
		t.Fatalf("choices %v sum to %d, exceeding the cap 10", in.Choices, sum)
	}
	// The 11-power option can never be part of a legal answer.
	for _, c := range in.Choices {
		if c == 0 {
			t.Fatalf("the bot picked the individually over-budget option 0")
		}
	}
}

// A KTarget decision whose budget can be met only by an offset -- an
// individually over-cap option beside a NEGATIVE-power one (Scourge of the
// Skyclaves -1 beside Polar Kraken 11 under a cap of 10) -- must still be
// answerable by the deterministic bot: the engine keeps the over-cap option
// offered because the offset could save it, so the naive full-width answer
// (both picks, 11 + (-1) = 10) is already valid and the bot produces the
// compensated selection without needing a repair.
func TestTargetBudgetNegativeOffsetProducesTheCompensatedSelection(t *testing.T) {
	b := Board{Cards: TableOf(map[state.ObjID]Card{})}
	d := decision.Decision{
		Player: 0,
		Kind:   decision.KTarget,
		Min:    0,
		Max:    2,
		MaxSum: 10,
		Options: []decision.Option{
			{Index: 0, Kind: "permanent", Obj: state.ObjID(1), Value: 11}, // over the cap alone
			{Index: 1, Kind: "permanent", Obj: state.ObjID(2), Value: -1}, // the offset
		},
	}
	in := Decide(b, &d, rng(1))
	if err := d.Validate(in); err != nil {
		t.Fatalf("bot answer %v failed Validate: %v", in.Choices, err)
	}
	if len(in.Choices) != 2 {
		t.Fatalf("choices = %v, want both options (the compensated selection)", in.Choices)
	}
	sum := 0
	for _, c := range in.Choices {
		sum += d.Options[c].Value
	}
	if sum != 10 {
		t.Fatalf("choices %v sum to %d, want the cap 10 exactly", in.Choices, sum)
	}
}

// When the naive full-width answer busts the budget (both positive picks
// sum past the cap), the Clamp repair must still hand back a set Validate
// accepts on a board carrying a negative option -- the repaired answer is
// valid even if it drops picks, never re-submitted and re-rejected.
func TestTargetBudgetNegativeOffsetRepairStaysValid(t *testing.T) {
	b := Board{Cards: TableOf(map[state.ObjID]Card{})}
	d := decision.Decision{
		Player: 0,
		Kind:   decision.KTarget,
		Min:    0,
		Max:    3,
		MaxSum: 10,
		Options: []decision.Option{
			{Index: 0, Kind: "permanent", Obj: state.ObjID(1), Value: 6},
			{Index: 1, Kind: "permanent", Obj: state.ObjID(2), Value: 6},
			{Index: 2, Kind: "permanent", Obj: state.ObjID(3), Value: -1},
		},
	}
	in := Decide(b, &d, rng(1))
	if err := d.Validate(in); err != nil {
		t.Fatalf("repaired bot answer %v failed Validate: %v", in.Choices, err)
	}
	sum := 0
	for _, c := range in.Choices {
		sum += d.Options[c].Value
	}
	if sum > d.MaxSum {
		t.Fatalf("choices %v sum to %d, exceeding the cap 10", in.Choices, sum)
	}
}

// A budget-less KTarget decision keeps the plain full-width answer
// byte-for-byte -- Option.Value is unset on every existing target ask, so
// no bot-answered game changes.
func TestTargetWithoutMaxSumUnchanged(t *testing.T) {
	b := Board{Cards: TableOf(map[state.ObjID]Card{})}
	d := decision.Decision{
		Player: 0,
		Kind:   decision.KTarget,
		Min:    0,
		Max:    3,
		Options: []decision.Option{
			{Index: 0, Kind: "permanent", Obj: state.ObjID(1)},
			{Index: 1, Kind: "permanent", Obj: state.ObjID(2)},
			{Index: 2, Kind: "permanent", Obj: state.ObjID(3)},
		},
	}
	in := Decide(b, &d, rng(1))
	if !reflect.DeepEqual(in.Choices, []int{0, 1, 2}) {
		t.Fatalf("choices = %v, want all three (full-width policy unchanged)", in.Choices)
	}
}

// A NEGATIVE budget (Budgeted, MaxTotalTargetPower$ below zero) busts even
// the empty answer, so the bot's repair must take enough negative options to
// reach it -- including one the fold alone skips because its own offset is
// not enough on its own (-1 does not reach -2 until the other -1 joins). A
// price-blind preference for the positive option must never come back as
// an answer Validate rejects (the livelock the shared FitRequired rule
// exists to prevent).
func TestTargetBudgetNegativeCapTakesTheOffsets(t *testing.T) {
	b := Board{Cards: TableOf(map[state.ObjID]Card{})}
	d := decision.Decision{
		Player:   0,
		Kind:     decision.KTarget,
		Min:      0,
		Max:      3,
		MaxSum:   -2,
		Budgeted: true,
		Options: []decision.Option{
			{Index: 0, Kind: "permanent", Obj: state.ObjID(1), Value: 1},
			{Index: 1, Kind: "permanent", Obj: state.ObjID(2), Value: -1},
			{Index: 2, Kind: "permanent", Obj: state.ObjID(3), Value: -1},
		},
	}
	for _, pref := range [][]int{{0}, {}, {0, 1}} {
		got := d.FitRequired(pref)
		if err := d.Validate(decision.Intent{Choices: got}); err != nil {
			t.Fatalf("FitRequired(%v) = %v failed Validate: %v", pref, got, err)
		}
	}
	in := Decide(b, &d, rng(1))
	if err := d.Validate(in); err != nil {
		t.Fatalf("bot answer %v failed Validate: %v", in.Choices, err)
	}
}

// A ZERO budget is a present budget only when Budgeted is set: the same
// options with Budgeted false keep the historical budget-less reading
// (MaxSum 0 = none), so no existing decision changes meaning.
func TestTargetBudgetZeroCapNeedsBudgeted(t *testing.T) {
	d := decision.Decision{Kind: decision.KTarget, Min: 0, Max: 2,
		Options: []decision.Option{
			{Index: 0, Kind: "permanent", Obj: state.ObjID(1), Value: 2},
			{Index: 1, Kind: "permanent", Obj: state.ObjID(2), Value: -1},
		}}
	if err := d.Validate(decision.Intent{Choices: []int{0}}); err != nil {
		t.Fatalf("budget-less decision rejected an answer: %v", err)
	}
	d.Budgeted = true
	if err := d.Validate(decision.Intent{Choices: []int{0}}); err == nil {
		t.Fatal("a total of 2 validated under a present budget of 0")
	}
	if err := d.Validate(decision.Intent{Choices: []int{0, 1}}); err == nil {
		t.Fatal("a total of 1 validated under a present budget of 0")
	}
	in := Decide(Board{Cards: TableOf(map[state.ObjID]Card{})}, &d, rng(1))
	if err := d.Validate(in); err != nil {
		t.Fatalf("bot answer %v failed Validate: %v", in.Choices, err)
	}
}
