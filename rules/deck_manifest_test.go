package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

func TestEngineOwnDeckIsDetachedGenesisData(t *testing.T) {
	card, diags := cards.ParseBytes("manifest.txt", []byte("Name:Manifest Card\nTypes:Creature\nPT:1/1\n"))
	if len(diags) != 0 {
		t.Fatal(diags)
	}
	card.Link()
	main := []*cards.Card{card, card}
	cfg := Config{Names: []string{"manifest"}, Decks: [][]*cards.Card{main}}
	e := New(cfg)
	want := e.OwnDeck(state.PlayerID(0))
	if want == nil || want.Name != "manifest" || len(want.Main) != 1 || want.Main[0].Name != "Manifest Card" || want.Main[0].Count != 2 {
		t.Fatalf("genesis manifest = %#v", want)
	}
	main[0] = nil
	if got := e.OwnDeck(0); got.Main[0].Count != 2 {
		t.Fatalf("config mutation changed engine manifest: %#v", got)
	}
	clone := e.Clone()
	copy := clone.OwnDeck(0)
	copy.Main[0].Name = "mutated"
	if got := e.OwnDeck(0); got.Main[0].Name != "Manifest Card" {
		t.Fatalf("clone manifest aliases original: %#v", got)
	}
	if e.OwnDeck(state.PlayerID(255)) != nil {
		t.Fatal("out-of-range seat received a manifest")
	}
}
