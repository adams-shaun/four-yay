package rules

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/cards"
)

func TestNewOwnsTokenMap(t *testing.T) {
	tok := card(t, "Name:Initial Token\nTypes:Creature\nPT:1/1\nOracle:x\n")
	const initialKey = "fixture:initial-token"
	callerTokens := map[string]*cards.Card{initialKey: tok}
	cfg := Config{Seed: 1, Names: []string{"a", "b"}, Tokens: callerTokens}

	e := New(cfg)
	if got := e.G.Tokens[initialKey]; got != tok {
		t.Fatalf("precondition: engine did not preserve initial token definition: got %p, want %p", got, tok)
	}
	if reflect.ValueOf(e.G.Tokens).Pointer() == reflect.ValueOf(callerTokens).Pointer() {
		t.Fatal("engine token map aliases caller's Config map")
	}

	const engineKey = "fixture:engine-only-token"
	e.G.Tokens[engineKey] = tok
	if _, ok := callerTokens[engineKey]; ok {
		t.Fatal("engine token insertion mutated caller's map")
	}

	const callerKey = "fixture:caller-only-token"
	callerTokens[callerKey] = tok
	if _, ok := e.G.Tokens[callerKey]; ok {
		t.Fatal("caller token insertion mutated engine's map")
	}
}
