package botpolicy

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

func TestDualTargetBudgetClampKeepsTheBotAnswerValid(t *testing.T) {
	b := Board{Cards: map[state.ObjID]Card{}}
	d := decision.Decision{Player: 0, Kind: decision.KTarget, Min: 0, Max: 4,
		MaxSum: 7, Budgeted: true, MaxSum2: 5, Budgeted2: true,
		Options: []decision.Option{
			{Index: 0, Kind: "permanent", Obj: 1, Value: 2, Value2: 4},
			{Index: 1, Kind: "permanent", Obj: 2, Value: 3, Value2: 1},
			{Index: 2, Kind: "permanent", Obj: 3, Value: 4, Value2: 3},
			{Index: 3, Kind: "permanent", Obj: 4, Value: 1, Value2: 1},
		}}
	if d.Options[0].Value == d.Options[0].Value2 {
		t.Fatal("precondition: currencies coincide")
	}
	if err := d.Validate(decision.Intent{Choices: []int{0, 1, 2, 3}}); err == nil {
		t.Fatal("precondition: full-width answer fits both budgets")
	}
	in := Decide(b, &d, rng(1))
	if err := d.Validate(in); err != nil {
		t.Fatalf("bot answer %v failed dual-budget Validate: %v", in.Choices, err)
	}
	var sum, sum2 int
	for _, c := range in.Choices {
		sum += d.Options[c].Value
		sum2 += d.Options[c].Value2
	}
	if sum > d.MaxSum || sum2 > d.MaxSum2 {
		t.Fatalf("choices %v totals (%d,%d), over budgets (%d,%d)", in.Choices, sum, sum2, d.MaxSum, d.MaxSum2)
	}
}
