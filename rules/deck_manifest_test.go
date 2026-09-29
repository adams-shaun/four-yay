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

// TestEngineOwnDeckCommandersMatchGenesis pins the single-source-of-truth rule:
// the manifest's commander identities are resolved through the SAME legality
// gate genesis seats command-zone objects through (legalCommandersFor), so an
// illegal Config that leaves the command zone empty cannot still name those
// cards as commanders in the manifest.
func TestEngineOwnDeckCommandersMatchGenesis(t *testing.T) {
	parse := func(name, types string) *cards.Card {
		t.Helper()
		c, diags := cards.ParseBytes("manifest-cmd.txt", []byte("Name:"+name+"\nTypes:"+types+"\nPT:1/1\n"))
		if len(diags) != 0 {
			t.Fatal(diags)
		}
		c.Link()
		return c
	}
	cmdrA := parse("Cmd A", "Legendary Creature")
	cmdrB := parse("Cmd B", "Legendary Creature")
	filler := parse("Filler", "Creature")
	deck := []*cards.Card{cmdrA, cmdrB, filler, filler}

	// A legal single commander: the manifest lists it and genesis seats it in
	// the command zone.
	legal := New(Config{Names: []string{"legal"}, Decks: [][]*cards.Card{deck}, Commanders: [][]int{{0}}, Format: FormatCommander})
	if got := legal.OwnDeck(0); got == nil || !reflect.DeepEqual(got.Commanders, []string{"Cmd A"}) {
		t.Fatalf("legal commander manifest = %#v", got)
	}
	if n := len(legal.G.Players[0].Commanders); n != 1 {
		t.Fatalf("precondition: genesis seated %d commanders, want 1", n)
	}

	// An illegal three-card set is rejected WHOLE: genesis seats none, so the
	// manifest must name none either.
	illegal := New(Config{Names: []string{"illegal"}, Decks: [][]*cards.Card{deck}, Commanders: [][]int{{0, 1, 2}}, Format: FormatCommander})
	if n := len(illegal.G.Players[0].Commanders); n != 0 {
		t.Fatalf("precondition: genesis seated %d commanders for an illegal set, want 0", n)
	}
	if got := illegal.OwnDeck(0); got == nil || len(got.Commanders) != 0 {
		t.Fatalf("illegal commander set still named in the manifest: %#v", got)
	}

	// A non-legendary single commander is likewise rejected.
	notLegend := New(Config{Names: []string{"plain"}, Decks: [][]*cards.Card{deck}, Commanders: [][]int{{2}}, Format: FormatCommander})
	if got := notLegend.OwnDeck(0); got == nil || len(got.Commanders) != 0 {
		t.Fatalf("non-legendary commander named in the manifest: %#v", got)
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
