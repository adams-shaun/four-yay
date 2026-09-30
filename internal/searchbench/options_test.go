package searchbench

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

func TestNamedOptionChoosesFirstEqualNameCopy(t *testing.T) {
	d := &decision.Decision{Seq: 17, Player: state.PlayerID(1), Kind: decision.KPriority, Options: []decision.Option{
		{Index: 3, Label: "Play Mountain"},
		{Index: 8, Label: "Play Mountain"},
	}}
	in, err := NamedOption(d, "Play ", "Mountain")
	if err != nil {
		t.Fatal(err)
	}
	if in.Seq != 17 || in.Player != 1 || len(in.Choices) != 1 || in.Choices[0] != 3 {
		t.Fatalf("intent = %#v, want first matching copy", in)
	}
}
