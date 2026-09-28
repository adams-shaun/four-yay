package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/decision"
)

// TestSameControllerClampFindsFeasibleLaterController pins the bot-repair
// path when the first represented controller cannot reach Min. The later
// controller's legal pair must also obey its Group and budget constraints.
func TestSameControllerClampFindsFeasibleLaterController(t *testing.T) {
	t.Parallel()
	d := &decision.Decision{
		Seq:                       7,
		Player:                    0,
		Kind:                      decision.KTarget,
		Min:                       2,
		Max:                       2,
		MaxSum:                    5,
		TargetsWithSameController: true,
		Options: []decision.Option{
			{Index: 0, Controller: 0, Value: 1}, // first input controller: insufficient
			{Index: 1, Controller: 1, Value: 4, Group: "exclusive"},
			{Index: 2, Controller: 1, Value: 3, Group: "exclusive"},
			{Index: 3, Controller: 1, Value: 1},
		},
	}
	if err := d.Validate(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{1, 3}}); err != nil {
		t.Fatalf("fixture precondition: later controller lacks a legal pair: %v", err)
	}
	clamped := botpolicy.Clamp(d, decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0, 1}})
	if err := d.Validate(clamped); err != nil {
		t.Fatalf("Clamp returned an invalid same-controller answer: %v (%+v)", err, clamped)
	}
	if len(clamped.Choices) != 2 || clamped.Choices[0] != 1 || clamped.Choices[1] != 3 {
		t.Fatalf("Clamp = %v, want later controller's budgeted, non-conflicting pair [1 3]", clamped.Choices)
	}
}
