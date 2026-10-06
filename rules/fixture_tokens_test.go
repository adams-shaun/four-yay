package rules

import (
	"maps"
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// fixtureTokenMap gives a fixture its own writable token table. Config and
// Game may share this returned map; the corpus registry must not.
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
		Tokens: fixtureTokenMap(reg.Tokens),
	}
	e := New(cfg)
	if reflect.ValueOf(e.G.Tokens).Pointer() != reflect.ValueOf(cfg.Tokens).Pointer() {
		t.Fatal("fixture setup: Config and Game must share the fixture token map")
	}
	if reflect.ValueOf(e.G.Tokens).Pointer() == reflect.ValueOf(reg.Tokens).Pointer() {
		t.Fatal("fixture setup: mutable fixture token map aliases the corpus registry")
	}
	tok := card(t, "Name:Fixture Token\nTypes:Creature\nPT:1/1\nOracle:x\n")
	if tok == nil || tok.Faces[0].Name != "Fixture Token" {
		t.Fatal("precondition: inline fixture token was not parsed")
	}
	e.G.Tokens[key] = tok
	if cfg.Tokens[key] != tok {
		t.Fatal("fixture insertion is not visible through the Config map")
	}
	if got := reg.Tokens[key]; got != nil {
		t.Fatalf("fixture token insertion mutated corpus registry: %p", got)
	}
}
