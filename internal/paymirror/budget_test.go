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

// TestBudgetExceededAfterOneIntent pins the deferred cutoff: the predicate
// allows the first intent and ends the game at the next between-intents
// boundary, so exactly one intent is submitted and the result is still the
// harness truncation. (Deterministic: no clock, no sleep.)
func TestBudgetExceededAfterOneIntent(t *testing.T) {
	cfg := rules.Config{Seed: 11, Names: []string{"rg", "ub"}, Decks: authoredDecks(t), Tokens: map[string]*cards.Card{}}
	checks := 0
	first := true
	g := PlayConfig(cfg, GameSpec{Seed: 11, Decks: []string{"authored-rg", "authored-ub"}, Policy: "bot"}, DriverOptions{
		BudgetExceeded: func() bool {
			checks++
			after := !first
			first = false
			return after
		},
	})
	if checks != 2 {
		t.Fatalf("budget predicate called %d times, want exactly twice (allow, then cut)", checks)
	}
	if g.Intents != 1 {
		t.Fatalf("budget cutoff occurred after %d intents, want exactly one submitted before cutoff", g.Intents)
	}
	if g.Err != "truncated: budget" {
		t.Fatalf("Err = %q, want truncated: budget", g.Err)
	}
}
