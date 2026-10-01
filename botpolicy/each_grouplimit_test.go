package botpolicy

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// The KChoose fill arms against a raised per-Group cap (Decision.GroupLimit,
// Forge's EACH multi-type grammar's per-type ChangeNum$): an option Group is
// one listed type and may contribute up to GroupLimit picks. A blind
// first-Max fill would spend the whole Max inside the FIRST Group and hand
// back an intent Decision.Validate's per-group cap rejects -- a deterministic
// bot that re-derives the same rejected answer forever (the livelock class
// the one-home rule exists for). The bot's answer must pass Validate on the
// board where the cap binds, derived from the same Decision.GroupCap read
// Validate enforces.

// TestBotFillHonoursPerGroupCapOnSearch runs the search arm's group-aware
// fill: options [three of group "0", two of group "1"], Max 4, GroupLimit 2
// ("EACH Forest & Plains", ChangeNum$ 2, 3 Forests and 2 Plains). The answer
// takes the first two of EACH group and Validate accepts it; the pre-fix
// blind fill ([0,1,2,3]) is rejected by the cap the engine enforces.
func TestBotFillHonoursPerGroupCapOnSearch(t *testing.T) {
	b := Board{Cards: TableOf(map[state.ObjID]Card{})}
	d := decision.Decision{
		Kind:       decision.KChoose,
		ResumeKind: "search",
		Min:        0,
		Max:        4,
		GroupLimit: 2,
		Options: []decision.Option{
			{Index: 0, Kind: "search", Obj: 1, Group: "0"},
			{Index: 1, Kind: "search", Obj: 2, Group: "0"},
			{Index: 2, Kind: "search", Obj: 3, Group: "0"},
			{Index: 3, Kind: "search", Obj: 4, Group: "1"},
			{Index: 4, Kind: "search", Obj: 5, Group: "1"},
		},
	}
	if d.GroupCap() != 2 {
		t.Fatal("precondition failed: the decision's effective per-group cap is not 2")
	}
	in := Decide(b, &d, rng(1))
	if err := d.Validate(in); err != nil {
		t.Fatalf("bot answer %v failed Validate: %v", in.Choices, err)
	}
	if want := []int{0, 1, 3, 4}; !intsEqual(in.Choices, want) {
		t.Fatalf("choices = %v, want the first two of EACH group %v", in.Choices, want)
	}
}

// TestBotFillHonoursPerGroupCapOnHiddenPick runs the dig/hand_move/
// hidden_pick arm's group-aware fill on the same shape (an EACH public-origin
// pick routes through it, not the search arm).
func TestBotFillHonoursPerGroupCapOnHiddenPick(t *testing.T) {
	b := Board{Cards: TableOf(map[state.ObjID]Card{})}
	d := decision.Decision{
		Kind:       decision.KChoose,
		ResumeKind: "hidden_pick",
		Min:        0,
		Max:        4,
		GroupLimit: 2,
		Options: []decision.Option{
			{Index: 0, Kind: "hidden_pick", Obj: 1, Group: "0"},
			{Index: 1, Kind: "hidden_pick", Obj: 2, Group: "0"},
			{Index: 2, Kind: "hidden_pick", Obj: 3, Group: "0"},
			{Index: 3, Kind: "hidden_pick", Obj: 4, Group: "1"},
			{Index: 4, Kind: "hidden_pick", Obj: 5, Group: "1"},
		},
	}
	in := Decide(b, &d, rng(1))
	if err := d.Validate(in); err != nil {
		t.Fatalf("bot answer %v failed Validate: %v", in.Choices, err)
	}
	if want := []int{0, 1, 3, 4}; !intsEqual(in.Choices, want) {
		t.Fatalf("choices = %v, want the first two of EACH group %v", in.Choices, want)
	}
}

// TestBotFillUngroupedOptionsUnchanged pins the unchanged half: the
// group-aware fill on an ungrouped decision is the historical first-Max take,
// byte-identical, so no existing ask's bot answer moved.
func TestBotFillUngroupedOptionsUnchanged(t *testing.T) {
	b := Board{Cards: TableOf(map[state.ObjID]Card{})}
	d := decision.Decision{
		Kind:       decision.KChoose,
		ResumeKind: "hidden_pick",
		Min:        0,
		Max:        2,
		Options: []decision.Option{
			{Index: 0, Kind: "hidden_pick", Obj: 1},
			{Index: 1, Kind: "hidden_pick", Obj: 2},
			{Index: 2, Kind: "hidden_pick", Obj: 3},
		},
	}
	in := Decide(b, &d, rng(1))
	if err := d.Validate(in); err != nil {
		t.Fatalf("bot answer %v failed Validate: %v", in.Choices, err)
	}
	if want := []int{0, 1}; !intsEqual(in.Choices, want) {
		t.Fatalf("choices = %v, want the historical first-Max take %v", in.Choices, want)
	}
}

func intsEqual(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
