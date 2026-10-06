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
	// New adopts the registry map by reference (no per-engine copy); the
	// fixture helper is what keeps a write off it.
	if reflect.ValueOf(e.G.Tokens).Pointer() != reflect.ValueOf(cfg.Tokens).Pointer() {
		t.Fatal("precondition: New copied Config.Tokens; the shared-map contract is gone")
	}
	tok := card(t, "Name:Fixture Token\nTypes:Creature\nPT:1/1\nOracle:x\n")
	if tok == nil || len(tok.Faces) == 0 || tok.Faces[0].Name != "Fixture Token" {
		t.Fatal("precondition: inline fixture token was not parsed")
	}
	setFixtureToken(e, key, tok)
	if e.G.Tokens[key] != tok {
		t.Fatal("fixture token was not registered on the engine")
	}
	if cfg.Tokens[key] != nil || reg.Tokens[key] != nil {
		t.Fatal("engine token insertion mutated caller's Config map or corpus registry")
	}
}

// setFixtureToken registers one fixture token on e. New adopts Config.Tokens
// by reference (often a shared corpus registry's map), so a fixture never
// writes e.G.Tokens in place: it gives the engine its own copy first.
func setFixtureToken(e *Engine, key string, def *cards.Card) {
	e.G.Tokens = fixtureTokenMap(e.G.Tokens)
	e.G.Tokens[key] = def
}
