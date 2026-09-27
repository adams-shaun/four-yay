package rules

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

func TestEngineCompiledTextSharesWithCloneAndFallsBack(t *testing.T) {
	c := card(t, "Name:Cache Test\nManaCost:1 U\nTypes:Creature Test\nPT:1/1\nA:AB$ Draw | Cost$ GWP 2B Sac<1/Creature> | ValidTgts$ Creature.YouCtrl+untapped\nOracle:x\n")
	e := New(Config{Names: []string{"you"}, Decks: [][]*cards.Card{{c}}})
	if e.compiledText == nil || e.compiledText.predicates == nil {
		t.Fatal("New did not build compiled text")
	}
	if got := e.specCtx(0, 0).PredicatePrograms; got != e.compiledText.predicates {
		t.Fatal("spec context did not carry compiled predicates")
	}
	raw := "GWP 2B Sac<1/Creature>"
	if got, want := e.parseCost(raw), ParseCost(raw); !reflect.DeepEqual(got, want) {
		t.Fatalf("cached cost = %#v, want %#v", got, want)
	}
	if got, want := e.parseCost("dynamic cost"), ParseCost("dynamic cost"); !reflect.DeepEqual(got, want) {
		t.Fatalf("fallback cost = %#v, want %#v", got, want)
	}
	clone := e.Clone()
	if clone.compiledText != e.compiledText {
		t.Fatal("clone did not share immutable compiled text")
	}
	again := New(Config{Names: []string{"you"}, Decks: [][]*cards.Card{{c}}})
	if again.compiledText != e.compiledText {
		t.Fatal("equivalent configurations rebuilt immutable compiled text")
	}
}

func TestEngineCompiledTextCacheSeparatesCardLayouts(t *testing.T) {
	deckA := card(t, "Name:Deck A\nTypes:Creature Test\nPT:1/1\nOracle:x\n")
	deckB := card(t, "Name:Deck B\nTypes:Creature Test\nPT:1/1\nOracle:x\n")
	tokenA := card(t, "Name:Token A\nTypes:Creature Test\nPT:1/1\nOracle:x\n")
	tokenB := card(t, "Name:Token B\nTypes:Creature Test\nPT:1/1\nOracle:x\n")

	base := New(Config{Decks: [][]*cards.Card{{deckA}}, Tokens: map[string]*cards.Card{"T": tokenA}})
	otherDeck := New(Config{Decks: [][]*cards.Card{{deckB}}, Tokens: map[string]*cards.Card{"T": tokenA}})
	if otherDeck.compiledText == base.compiledText {
		t.Fatal("different deck card reused immutable compiled text")
	}
	otherToken := New(Config{Decks: [][]*cards.Card{{deckA}}, Tokens: map[string]*cards.Card{"T": tokenB}})
	if otherToken.compiledText == base.compiledText {
		t.Fatal("different token card reused immutable compiled text")
	}
}

// A fuzzer starts every game from fresh decks, so every configuration is a
// miss: the memo must stay bounded instead of pinning each game's compiled
// text (and every token script's) for the life of the process.
func TestEngineCompiledTextCacheStaysBounded(t *testing.T) {
	for i := 0; i < 3*compiledTextCacheLimit; i++ {
		c := card(t, fmt.Sprintf("Name:Bound %d\nTypes:Creature Test\nPT:1/1\nOracle:x\n", i))
		New(Config{Decks: [][]*cards.Card{{c}}})
		compiledTextCache.Lock()
		n, total := compiledTextCache.n, 0
		for _, entries := range compiledTextCache.entries {
			total += len(entries)
		}
		compiledTextCache.Unlock()
		if n > compiledTextCacheLimit || total != n {
			t.Fatalf("after %d distinct configurations: count %d, entries %d (limit %d)", i+1, n, total, compiledTextCacheLimit)
		}
	}
	// A repeated configuration still hits after the wholesale drop.
	c := card(t, "Name:Bound Again\nTypes:Creature Test\nPT:1/1\nOracle:x\n")
	a := New(Config{Decks: [][]*cards.Card{{c}}})
	b := New(Config{Decks: [][]*cards.Card{{c}}})
	if a.compiledText != b.compiledText {
		t.Fatal("a repeated configuration rebuilt its compiled text")
	}
}

func TestEngineMatchesSpecFromCarriesCompiledText(t *testing.T) {
	c := card(t, "Name:Compiled Match\nTypes:Creature Test\nPT:1/1\nOracle:x\n")
	e := New(Config{Names: []string{"you"}, Decks: [][]*cards.Card{{c}}})
	var id state.ObjID
	for _, o := range e.G.Objs {
		if o.Card == c {
			id = o.ID
			break
		}
	}
	if id == 0 {
		t.Fatal("configured card was not added to the game")
	}
	if !e.matchesSpecFrom("Creature.YouCtrl+untapped", id, 0, id) {
		t.Fatal("engine-owned match did not preserve configured filter semantics")
	}
}

func TestEngineLoyaltyAbilityUsesConfiguredCost(t *testing.T) {
	c := card(t, "Name:Loyalty Cache\nTypes:Planeswalker Test\nLoyalty:3\nA:AB$ Pump | Cost$ AddCounter<1/LOYALTY>\nOracle:x\n")
	e := New(Config{Names: []string{"you"}, Decks: [][]*cards.Card{{c}}})
	if !e.isLoyaltyAbility(c.Faces[0].Abilities[0]) {
		t.Fatal("configured loyalty counter cost was not recognized")
	}
}
