package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// TestCloneOwnsTheHeldBackOpeningExileAsk pins that a clone taken while the
// opening-hand round holds back a Gemstone-shape exile ask (its preceding
// entry posed a decision first) owns its own copy of that Decision: Engine.ask
// stamps the decision it poses, so a shared pointer would let one engine's
// pose rewrite the other's pending decision.
func TestCloneOwnsTheHeldBackOpeningExileAsk(t *testing.T) {
	names, decks := testutil.SampleDecks(t, 2)
	e := New(Config{Seed: 3, Names: names, Decks: decks})
	e.Advance()
	e.opening.exileAsk = &decision.Decision{Player: 0, Kind: decision.KChoose, Min: 1, Max: 1,
		Prompt:  "Exile a card from your hand",
		Options: []decision.Option{{Index: 0, Kind: "opening_exile", Obj: 1, Label: "a"}}}

	c := e.Clone()
	if c.opening.exileAsk == nil {
		t.Fatal("clone dropped the held-back opening exile ask")
	}
	if c.opening.exileAsk == e.opening.exileAsk {
		t.Fatal("clone shares the held-back opening exile ask's Decision with the original")
	}
	c.opening.exileAsk.Seq = 77
	c.opening.exileAsk.Options[0].Label = "mutated"
	if e.opening.exileAsk.Seq == 77 || e.opening.exileAsk.Options[0].Label == "mutated" {
		t.Fatal("clone's held-back exile ask writes through to the original's")
	}
}
