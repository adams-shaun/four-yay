package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
)

func TestNewOwnsTokenMap(t *testing.T) {
	token := card(t, "Name:Ownership token\nTypes:Creature\nPT:1/1\nOracle:x\n")
	callerTokens := map[string]*cards.Card{"original": token}
	e := New(Config{Seed: 1, Names: []string{"a", "b"}, Tokens: callerTokens})

	if e.G.Tokens == nil || e.G.Tokens["original"] != token {
		t.Fatalf("precondition: engine token map missing original definition")
	}
	if len(callerTokens) != 1 || callerTokens["original"] != token {
		t.Fatalf("precondition: caller token map changed during New")
	}
	if e.G.Tokens == nil {
		t.Fatal("engine token map is nil")
	}

	callerTokens["caller-only"] = token
	if _, ok := e.G.Tokens["caller-only"]; ok {
		t.Fatal("caller mutation leaked into engine token map")
	}
	engineOnly := card(t, "Name:Engine-only token\nTypes:Creature\nPT:2/2\nOracle:x\n")
	e.G.Tokens["engine-only"] = engineOnly
	if _, ok := callerTokens["engine-only"]; ok {
		t.Fatal("engine mutation leaked into caller token map")
	}
}
