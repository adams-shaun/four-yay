package botpolicy

import (
	"github.com/adams-shaun/gorge/decision"
	"reflect"
	"testing"
)

func TestDigMultipleBotAnswerValidatesWithOverlappingTypes(t *testing.T) {
	d := &decision.Decision{Kind: decision.KChoose, Player: 0, Max: 2, Min: 0, DistinctTypePicks: true, Options: []decision.Option{
		{Index: 0, Kind: "digmultiple", SetProps: []string{"creature"}},
		{Index: 1, Kind: "digmultiple", SetProps: []string{"creature"}},
		{Index: 2, Kind: "digmultiple", SetProps: []string{"creature", "land"}},
	}}
	if d.DistinctTypesFit([]int{0, 1}) || !d.DistinctTypesFit([]int{0, 2}) {
		t.Fatal("precondition: two creatures clash, dual-type card can take land slot")
	}
	if err := d.Validate(decision.Intent{Player: 0, Choices: []int{0, 1}}); err == nil {
		t.Fatal("validator accepted two creature-only cards for one type")
	}
	in := Decide(Board{}, d, rng(1))
	if err := d.Validate(in); err != nil {
		t.Fatalf("bot answer %v rejected: %v", in.Choices, err)
	}
	if !reflect.DeepEqual(in.Choices, []int{0, 2}) {
		t.Fatalf("bot picks %v, want first creature and dual-type land", in.Choices)
	}
	fixed := Clamp(d, decision.Intent{Player: 0, Choices: []int{0, 1, 2}})
	if err := d.Validate(fixed); err != nil {
		t.Fatalf("clamp %v rejected: %v", fixed.Choices, err)
	}
}
