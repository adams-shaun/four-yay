package rules

import (
	"maps"
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// fixtureTokenMap gives a fixture its own writable token table.
func fixtureTokenMap(tokens map[string]*cards.Card) map[string]*cards.Card {
	cloned := maps.Clone(tokens)
	if cloned == nil {
		return map[string]*cards.Card{}
	}
	return cloned
}

func TestFixtureTokenMapDoesNotMutateCorpusRegistry(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	const key = "fixture:isolated-token-map-regression"
	if _, exists := reg.Tokens[key]; exists {
		t.Fatalf("precondition: corpus already contains fixture key %q", key)
	}
	cfg := Config{
		Seed:   1,
		Names:  []string{"a", "b"},
		Decks:  [][]*cards.Card{mountainDeck(t, 40), mountainDeck(t, 40)},
		Tokens: reg.Tokens,
	}
	e := New(cfg)
	if e.G.Tokens == nil || len(e.G.Tokens) != len(reg.Tokens) || e.G.Tokens[key] != nil {
		t.Fatal("precondition: New did not preserve the registry token definitions")
	}
	if reflect.ValueOf(e.G.Tokens).Pointer() == reflect.ValueOf(cfg.Tokens).Pointer() {
		t.Fatal("engine token map aliases the caller's Config map")
	}
	tok := card(t, "Name:Fixture Token\nTypes:Creature\nPT:1/1\nOracle:x\n")
	if tok == nil || len(tok.Faces) == 0 || tok.Faces[0].Name != "Fixture Token" {
		t.Fatal("precondition: inline fixture token was not parsed")
	}
	e.G.Tokens[key] = tok
	if cfg.Tokens[key] != nil || reg.Tokens[key] != nil {
		t.Fatal("engine token insertion mutated caller's Config map or corpus registry")
	}
}
