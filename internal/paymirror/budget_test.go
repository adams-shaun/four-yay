package paymirror

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/rules"
)

func TestBudgetExceededTruncatesBetweenIntents(t *testing.T) {
	cfg := rules.Config{Seed: 11, Names: []string{"rg", "ub"}, Decks: authoredDecks(t), Tokens: map[string]*cards.Card{}}
	checks := 0
	g := PlayConfig(cfg, GameSpec{Seed: 11, Decks: []string{"authored-rg", "authored-ub"}, Policy: "bot"}, DriverOptions{
		BudgetExceeded: func() bool {
			checks++
			return true
		},
	})
	if checks != 1 {
		t.Fatalf("budget predicate called %d times, want once before first intent", checks)
	}
	if g.Intents != 0 {
		t.Fatalf("budget cutoff occurred after %d intents, want before first intent", g.Intents)
	}
	if g.Err != "truncated: budget" {
		t.Fatalf("Err = %q, want truncated: budget", g.Err)
	}
}
