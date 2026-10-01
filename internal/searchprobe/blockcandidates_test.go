package searchprobe

import (
	"fmt"
	"testing"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/decision"
)

// blockGuard binds the bot's block guard to an empty two-seat board, the way
// searchseat binds it to the deciding seat's real board.
func blockGuard(d *decision.Decision) func([]int) []int {
	b := botpolicy.NewBoard(2)
	b.Life.Set(d.Player, 20)
	return func(c []int) []int { return botpolicy.LegalBlockChoices(b, d, c) }
}

func blockKeys(t *testing.T, d *decision.Decision, got []decision.Intent) []string {
	t.Helper()
	var keys []string
	seen := map[string]bool{}
	for _, in := range got {
		if err := d.Validate(in); err != nil {
			t.Fatalf("invalid candidate %v: %v", in.Choices, err)
		}
		k := fmt.Sprint(in.Choices)
		if seen[k] {
			t.Fatalf("duplicate candidate %v", in.Choices)
		}
		seen[k] = true
		keys = append(keys, k)
	}
	return keys
}

// A blocker offered against two attackers is one Group: adding its other
// pair must REPLACE the bot's pair, never produce a double block Validate
// refuses.
func TestBlockCandidatesGroupCappedBlocker(t *testing.T) {
	d := &decision.Decision{Kind: decision.KBlockers, Seq: 7, Player: 1, Min: 0, Max: 3, Options: []decision.Option{
		{Index: 0, Kind: "block", Obj: 10, Attacker: 1, Group: "blocker:10"},
		{Index: 1, Kind: "block", Obj: 10, Attacker: 2, Group: "blocker:10"},
		{Index: 2, Kind: "block", Obj: 11, Attacker: 1, Group: "blocker:11"},
	}}
	bot := decision.Intent{Seq: 7, Player: 1, Choices: []int{0}}
	got := BlockCandidates(d, bot, 8, blockGuard(d))
	keys := blockKeys(t, d, got)
	want := []string{"[0]", "[]", "[1]", "[0 2]"}
	if fmt.Sprint(keys) != fmt.Sprint(want) {
		t.Fatalf("candidates %v, want %v", keys, want)
	}
	if capped := BlockCandidates(d, bot, 2, blockGuard(d)); len(capped) != 2 {
		t.Fatalf("limit ignored: %d", len(capped))
	}
	if BlockCandidates(&decision.Decision{Kind: decision.KAttackers}, bot, 8, nil) != nil {
		t.Fatal("non-blockers decision produced candidates")
	}
}

// A Min$ 2 (menace-style) attacker: adding ONE blocker to it is a partial
// team the engine rejects (CR 509.1a) and the bot's guard would strip, so the
// single-block toggle must be dropped rather than offered.
func TestBlockCandidatesDropsPartialMinBlockersTeam(t *testing.T) {
	d := &decision.Decision{Kind: decision.KBlockers, Seq: 3, Player: 1, Min: 0, Max: 3, Options: []decision.Option{
		{Index: 0, Kind: "block", Obj: 10, Attacker: 1, Group: "blocker:10", MinBlockers: 2},
		{Index: 1, Kind: "block", Obj: 11, Attacker: 1, Group: "blocker:11", MinBlockers: 2},
		{Index: 2, Kind: "block", Obj: 12, Attacker: 2, Group: "blocker:12"},
	}}
	bot := decision.Intent{Seq: 3, Player: 1, Choices: []int{2}}
	got := BlockCandidates(d, bot, 8, blockGuard(d))
	keys := blockKeys(t, d, got)
	want := []string{"[2]", "[]"}
	if fmt.Sprint(keys) != fmt.Sprint(want) {
		t.Fatalf("candidates %v, want %v", keys, want)
	}
	// Without the guard the partial teams would be offered: the guard, not
	// Validate, is what keeps them out.
	if n := len(BlockCandidates(d, bot, 8, nil)); n != 4 {
		t.Fatalf("unguarded candidates %d, want 4", n)
	}
}

// A blocker that must block (CR 509.1c): the "no blocks" candidate is the
// least legal declaration, which still carries the required pair.
func TestBlockCandidatesNoBlockKeepsRequiredBlocker(t *testing.T) {
	d := &decision.Decision{Kind: decision.KBlockers, Seq: 5, Player: 1, Min: 0, Max: 2, Options: []decision.Option{
		{Index: 0, Kind: "block", Obj: 10, Attacker: 1, Group: "blocker:10", Required: true, BlockMust: true},
		{Index: 1, Kind: "block", Obj: 11, Attacker: 1, Group: "blocker:11"},
	}}
	bot := decision.Intent{Seq: 5, Player: 1, Choices: []int{0, 1}}
	got := BlockCandidates(d, bot, 8, blockGuard(d))
	keys := blockKeys(t, d, got)
	want := []string{"[0 1]", "[0]"}
	if fmt.Sprint(keys) != fmt.Sprint(want) {
		t.Fatalf("candidates %v, want %v", keys, want)
	}
	for _, in := range got {
		if d.RequiredChosen(in.Choices) < d.RequiredQuota() {
			t.Fatalf("candidate %v drops the required blocker", in.Choices)
		}
	}
}
