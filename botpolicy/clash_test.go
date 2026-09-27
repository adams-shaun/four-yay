package botpolicy

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/state"
)

func TestClashPlacementBotAnswerValidates(t *testing.T) {
	d := effects.ClashPlacementDecision(1, 7, &cards.SA{}, []state.PlayerID{0, 1}, []state.ObjID{8, 9}, 0, 1, 9)
	d.Seq = 1
	in := Decide(Board{}, d, rng(1))
	if err := d.Validate(in); err != nil {
		t.Fatalf("bot answer %+v rejected by Decision.Validate: %v", in, err)
	}
	if len(in.Choices) != 1 || in.Choices[0] != 0 {
		t.Fatalf("no-host-safe bot answer = %v, want bottom option 0", in.Choices)
	}
}
