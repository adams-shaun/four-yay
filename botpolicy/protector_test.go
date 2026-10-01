package botpolicy

import (
	"math/rand/v2"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// TestChooseProtectorBotPrefersLowestLife pins the CR 310.10 protector arm:
// a bot names the offered opponent closest to losing, not the clamp fallback
// (option 0). The hand-built Board makes the two offered lives differ, so the
// assertion can only hold if the arm read them.
func TestChooseProtectorBotPrefersLowestLife(t *testing.T) {
	d := &decision.Decision{Kind: decision.KChoose, Player: 0, Min: 1, Max: 1,
		Options: []decision.Option{
			{Index: 0, Kind: "protector", Player: 1},
			{Index: 1, Kind: "protector", Player: 2},
		}}
	b := Board{Life: TableOf(map[state.PlayerID]int32{1: 12, 2: 4})}
	if b.Life.Get(1) == b.Life.Get(2) {
		t.Fatal("precondition: offered life totals must differ")
	}
	got := Decide(b, d, rand.New(rand.NewPCG(1, 2)))
	if len(got.Choices) != 1 || got.Choices[0] != 1 {
		t.Fatalf("bot protector choice = %v, want option 1 (the lower-life opponent)", got.Choices)
	}
	// The arm's answer must satisfy the decision's own legal-answer rule --
	// the one shared Constraint (validate/clamp/arm), so a future tightening
	// of Validate cannot leave the arm livelocked on a rejected answer.
	if err := d.Validate(got); err != nil {
		t.Fatalf("bot answer %v rejected by Decision.Validate: %v", got.Choices, err)
	}
}

// TestChooseProtectorBotArmReadsBoardFromGame proves the arm reads real game
// state rather than a hand-built Board: BoardFromGame must populate every
// player's life, and the choice must follow it. With seat 1 at 20 and seat 2
// at 3, the arm must name seat 2 (option 1); the clamp fallback the arm
// replaces would name option 0.
func TestChooseProtectorBotArmReadsBoardFromGame(t *testing.T) {
	g := state.NewGame([]string{"a", "b", "c"})
	g.Players[1].Life = 20
	g.Players[2].Life = 3

	b := BoardFromGame(g, stubChars{}, 0)
	if got := b.Life.Get(1); got != 20 {
		t.Fatalf("precondition: BoardFromGame life for seat 1 = %d, want 20 (Life must be populated)", got)
	}
	if got := b.Life.Get(2); got != 3 {
		t.Fatalf("precondition: BoardFromGame life for seat 2 = %d, want 3 (Life must be populated)", got)
	}
	if b.Life.Get(1) == b.Life.Get(2) {
		t.Fatal("precondition: the two offered opponents must have different life")
	}

	d := &decision.Decision{Kind: decision.KChoose, Player: 0, Min: 1, Max: 1,
		Options: []decision.Option{
			{Index: 0, Kind: "protector", Player: 1},
			{Index: 1, Kind: "protector", Player: 2},
		}}
	got := Decide(b, d, rand.New(rand.NewPCG(1, 2)))
	if len(got.Choices) != 1 || got.Choices[0] != 1 {
		t.Fatalf("bot protector choice from BoardFromGame = %v, want option 1 (seat 2, life 3)", got.Choices)
	}
	if err := d.Validate(got); err != nil {
		t.Fatalf("bot answer %v rejected by Decision.Validate: %v", got.Choices, err)
	}
}
