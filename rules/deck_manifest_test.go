package rules

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/deck"
	"github.com/adams-shaun/gorge/state"
)

// TestEngineOwnDeckIsDetachedGenesisData pins that rules.New builds the seat
// manifest from the Genesis configuration (Config.Decks/Sideboards/Commanders
// and Config.Names as the identity), that it is canonically ordered, and that
// every published copy is detached from engine storage.
func TestEngineOwnDeckIsDetachedGenesisData(t *testing.T) {
	parse := func(name string) *cards.Card {
		t.Helper()
		c, diags := cards.ParseBytes("manifest.txt", []byte("Name:"+name+"\nTypes:Creature\nPT:1/1\n"))
		if len(diags) != 0 {
			t.Fatal(diags)
		}
		c.Link()
		return c
	}
	alpha, beta, gamma := parse("Manifest Alpha"), parse("Manifest Beta"), parse("Manifest Gamma")
	main := []*cards.Card{gamma, alpha, beta, alpha}
	sideboard := []*cards.Card{beta, beta}
	// Declared commander order is main[2] then main[1] = Beta then Alpha: a
	// distinct, non-alphabetical order proves the field preserves the declared
	// index order rather than sorting.
	cfg := Config{
		Names:      []string{"manifest"},
		Decks:      [][]*cards.Card{main},
		Sideboards: [][]*cards.Card{sideboard},
		Commanders: [][]int{{2, 1}},
	}
	e := New(cfg)
	got := e.OwnDeck(state.PlayerID(0))
	if got == nil {
		t.Fatal("New built no genesis manifest")
	}
	want := &deck.Manifest{
		Name:       "manifest",
		Main:       []deck.ManifestRow{{Name: "Manifest Alpha", Count: 2}, {Name: "Manifest Beta", Count: 1}, {Name: "Manifest Gamma", Count: 1}},
		Sideboard:  []deck.ManifestRow{{Name: "Manifest Beta", Count: 2}},
		Commanders: []string{"Manifest Beta", "Manifest Alpha"},
	}
	if !reflect.DeepEqual(*got, *want) {
		t.Fatalf("genesis manifest = %#v, want %#v", got, want)
	}
	// Mutating the Config's deck slice after construction changes nothing.
	main[0] = nil
	sideboard[0] = nil
	if row := gotRow(e.OwnDeck(0).Main, "Manifest Gamma"); row == nil || row.Count != 1 {
		t.Fatalf("config mutation changed engine manifest: %#v", e.OwnDeck(0))
	}
	// A published copy owns its storage: mutating it cannot reach the engine,
	// and a Clone carries the same manifest.
	copy := e.OwnDeck(0)
	copy.Main[0].Name = "mutated"
	copy.Commanders[0] = "mutated"
	if row := gotRow(e.OwnDeck(0).Main, "Manifest Alpha"); row == nil {
		t.Fatalf("published manifest mutation reached engine storage: %#v", e.OwnDeck(0))
	}
	if !reflect.DeepEqual(e.Clone().OwnDeck(0), e.OwnDeck(0)) {
		t.Fatal("clone manifest differs from the original")
	}
	// An out-of-range seat has no manifest.
	if e.OwnDeck(state.PlayerID(255)) != nil {
		t.Fatal("out-of-range seat received a manifest")
	}
}

func gotRow(rows []deck.ManifestRow, name string) *deck.ManifestRow {
	for i := range rows {
		if rows[i].Name == name {
			return &rows[i]
		}
	}
	return nil
}
