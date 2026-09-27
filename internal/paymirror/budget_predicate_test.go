package paymirror

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/rules"
)

func TestBudgetPredicateDoesNotChangeGame(t *testing.T) {
	cfg := rules.Config{Seed: 11, Names: []string{"rg", "ub"}, Decks: authoredDecks(t), Tokens: map[string]*cards.Card{}}
	spec := GameSpec{Seed: 11, Decks: []string{"authored-rg", "authored-ub"}, Policy: "bot"}
	withoutBudget := PlayConfig(cfg, spec, DriverOptions{Control: true, MaxTurns: 40})

	checks := 0
	withBudget := PlayConfig(cfg, spec, DriverOptions{
		Control:  true,
		MaxTurns: 40,
		BudgetExceeded: func() bool {
			checks++
			return false
		},
	})

	if withoutBudget.Intents <= 0 {
		t.Fatalf("no-budget game processed %d intents; comparison would be vacuous", withoutBudget.Intents)
	}
	if checks <= 0 {
		t.Fatalf("budget predicate called %d times; diagnostic predicate was not exercised", checks)
	}
	if checks < withBudget.Intents-1 || checks > withBudget.Intents {
		t.Fatalf("budget predicate called %d times for %d processed intents; want one check per intent boundary", checks, withBudget.Intents)
	}
	if withoutBudget.Err != "" || withBudget.Err != "" {
		t.Fatalf("game errors: without budget %q, with budget %q", withoutBudget.Err, withBudget.Err)
	}
	if withoutBudget.Intents != withBudget.Intents || withoutBudget.Turns != withBudget.Turns || withoutBudget.Over != withBudget.Over {
		t.Fatalf("game outcome changed: without budget {intents:%d turns:%d over:%v}, with budget {intents:%d turns:%d over:%v}",
			withoutBudget.Intents, withoutBudget.Turns, withoutBudget.Over,
			withBudget.Intents, withBudget.Turns, withBudget.Over)
	}
	if !reflect.DeepEqual(withoutBudget.Reports, withBudget.Reports) {
		t.Fatalf("mirror reports changed with a non-expiring budget predicate")
	}
}
