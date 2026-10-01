package botpolicy

import (
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
	"testing"
)

func TestVoteCardPolicyChoosesBestPermanentAndValidates(t *testing.T) {
	d := &decision.Decision{Player: 0, Kind: decision.KChoose, Min: 1, Max: 1, ResumeKind: "vote", Options: []decision.Option{
		{Index: 0, Kind: "vote_card", Obj: 1}, {Index: 1, Kind: "vote_card", Obj: 2},
	}}
	b := Board{Cards: TableOf(map[state.ObjID]Card{1: {CMC: 1}, 2: {CMC: 7}})}
	in := Decide(b, d, rng(1))
	if len(in.Choices) != 1 || in.Choices[0] != 1 {
		t.Fatalf("vote card choice = %v, want best permanent index 1", in.Choices)
	}
	if err := d.Validate(in); err != nil {
		t.Fatalf("bot vote answer rejected: %v", err)
	}
}
