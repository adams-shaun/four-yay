package view_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/replay"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

func TestOwnDeckIsOnlyInItsSeatProjection(t *testing.T) {
	parse := func(name string) *cards.Card {
		t.Helper()
		c, diags := cards.ParseBytes("own-deck.txt", []byte("Name:"+name+"\nTypes:Creature\nPT:1/1\n"))
		if len(diags) != 0 {
			t.Fatal(diags)
		}
		c.Link()
		return c
	}
	alpha, beta := parse("Private Alpha"), parse("Private Beta")
	cfg := rules.Config{Seed: 17, Names: []string{"alpha-list", "beta-list"}, Decks: [][]*cards.Card{{alpha, alpha, alpha, alpha, alpha, alpha, alpha, alpha}, {beta, beta, beta, beta, beta, beta, beta, beta}}}
	e := rules.New(cfg)
	before := e.OwnDeck(0)
	e.Advance()
	if got := e.OwnDeck(0); !reflect.DeepEqual(got, before) {
		t.Fatalf("manifest changed across opening shuffle/draw: before=%#v after=%#v", before, got)
	}
	replayed, err := replay.Replay(e.L, cfg)
	if err != nil {
		t.Fatalf("replay after opening shuffle/draw: %v", err)
	}
	if got := replayed.OwnDeck(0); !reflect.DeepEqual(got, before) {
		t.Fatalf("replay manifest = %#v, want %#v", got, before)
	}
	seat0 := view.ProjectFor(e.G, e, 0, view.Seat, (*decision.Decision)(nil))
	if seat0.OwnDeck == nil || seat0.OwnDeck.Name != "alpha-list" || len(seat0.OwnDeck.Main) != 1 || seat0.OwnDeck.Main[0].Count != 8 {
		t.Fatalf("seat view manifest = %#v", seat0.OwnDeck)
	}
	seat1 := view.ProjectFor(e.G, e, 1, view.Seat, nil)
	if seat1.OwnDeck == nil || seat1.OwnDeck.Main[0].Name != "Private Beta" {
		t.Fatalf("seat 1 manifest = %#v", seat1.OwnDeck)
	}
	for _, vis := range []view.Visibility{view.Public, view.Omniscient} {
		v := view.ProjectFor(e.G, e, view.NoSeat, vis, nil)
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		if v.OwnDeck != nil || strings.Contains(string(b), "own_deck") {
			t.Fatalf("%s projection exposed manifest: %s", vis, b)
		}
	}
	b, err := json.Marshal(seat0)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"own_deck"`) || !strings.Contains(string(b), "Private Alpha") || strings.Contains(string(b), "Private Beta") {
		t.Fatalf("seat JSON does not contain only its own manifest: %s", b)
	}
	shuffled, drawn := false, false
	for _, ev := range e.L.Events {
		shuffled = shuffled || ev.Kind == events.Shuffle
		drawn = drawn || ev.Kind == events.Draw
	}
	if !shuffled || !drawn {
		t.Fatalf("precondition: genesis did not exercise shuffle and draw (shuffle=%v draw=%v)", shuffled, drawn)
	}
	if got := e.Clone().OwnDeck(0); !reflect.DeepEqual(got, before) {
		t.Fatalf("clone manifest = %#v, want %#v", got, before)
	}
	copy := e.OwnDeck(state.PlayerID(0))
	copy.Main[0].Name = "changed"
	if e.OwnDeck(0).Main[0].Name != "Private Alpha" {
		t.Fatal("published manifest mutation reached engine storage")
	}
}
