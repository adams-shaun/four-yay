package rules_test

// The lifecycle half of the seat-deck-manifest acceptance (ticket
// seat-deck-03-acceptance): the manifest is genesis data, so it must be
// identical before and after the opening shuffle, a whole played game, and a
// replay at EVERY recorded sequence while the library it describes is dealt,
// shuffled, drawn from and searched. Replay-at-sequence stands in for the
// host's /undo, which rewinds a table to a prior sequence; ReplayTo rebuilds
// the engine at that sequence from the log.

import (
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/deck"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/replay"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/seat"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// acceptanceDeck builds the compact fixture the brief asks for on top of
// testutil's inline, playable sample deck (Ruling P9: no Forge scripts):
// duplicate main-deck names, one appended commander, and a sideboard.
func acceptanceDeck(t *testing.T, seatIndex int) (main []*cards.Card, side []*cards.Card, commanderIndex int) {
	t.Helper()
	_, decks := testutil.SampleDecks(t, 2)
	main = append([]*cards.Card(nil), decks[seatIndex]...)
	legend, diags := cards.ParseBytes("acceptance-legend.txt", []byte("Name:Acceptance Legend\nTypes:Legendary Creature\nPT:4/4\nOracle:x\n"))
	if len(diags) != 0 {
		t.Fatalf("parse acceptance legend: %v", diags)
	}
	legend.Link()
	commanderIndex = len(main)
	main = append(main, legend)
	side = []*cards.Card{legend, legend}
	return main, side, commanderIndex
}

// TestSeatDeckManifestStaticThroughFullGameAndReplay is the lifecycle leaf.
func TestSeatDeckManifestStaticThroughFullGameAndReplay(t *testing.T) {
	t.Parallel()
	main0, side0, cmd0 := acceptanceDeck(t, 0)
	main1, side1, cmd1 := acceptanceDeck(t, 1)
	cfg := rules.Config{
		Seed:       7788,
		Names:      []string{"lifecycle-owner", "lifecycle-opponent"},
		Decks:      [][]*cards.Card{main0, main1},
		Sideboards: [][]*cards.Card{side0, side1},
		Commanders: [][]int{{cmd0}, {cmd1}},
	}
	e := rules.New(cfg)
	genesis := e.OwnDeck(0)
	if genesis == nil {
		t.Fatal("precondition: genesis built no manifest")
	}

	// Precondition: the fixture really does have duplicate main-deck names
	// and a sideboard, or the "static" claim below would be about a trivial
	// list.
	dupes := false
	for _, r := range genesis.Main {
		if r.Count > 1 {
			dupes = true
		}
	}
	if !dupes || len(genesis.Sideboard) == 0 {
		t.Fatalf("precondition: fixture manifest is not the compact duplicate/sideboard shape: %#v", genesis)
	}

	e.Advance()
	sawShuffle, sawDraw := false, false
	for _, ev := range e.L.Events {
		sawShuffle = sawShuffle || ev.Kind == events.Shuffle
		sawDraw = sawDraw || ev.Kind == events.Draw
	}
	if !sawShuffle || !sawDraw {
		t.Fatalf("precondition: genesis did not shuffle+draw (shuffle=%v draw=%v)", sawShuffle, sawDraw)
	}
	if got := e.OwnDeck(0); !sameManifest(got, genesis) {
		t.Fatalf("manifest changed across the opening shuffle/draw: %#v vs %#v", got, genesis)
	}

	// Play a whole deterministic bot game.
	b := seat.NewBot(11)
	ctx := context.Background()
	n := 0
	for !e.G.Over && e.Pending() != nil && n < 400000 {
		d := e.Pending()
		v := view.Project(e.G, e, d.Player, d)
		intent, err := b.Decide(ctx, v, *d)
		if err != nil {
			t.Fatalf("intent %d: %v", n, err)
		}
		if err := e.Submit(intent); err != nil {
			t.Fatalf("intent %d: %v", n, err)
		}
		n++
	}
	if !e.G.Over {
		t.Fatalf("precondition: game did not finish after %d intents", n)
	}
	if got := e.OwnDeck(0); !sameManifest(got, genesis) {
		t.Fatalf("manifest changed across a full game: %#v vs %#v", got, genesis)
	}

	// The manifest is genesis data, so a replay at every recorded prefix
	// carries the identical manifest, while the library underneath moves.
	//
	// Walk rebuilds the engine ONCE and calls visit at every intent boundary
	// (the same state ReplayTo(l, cfg, k) returns at boundary k, via the same
	// internal walk), so this checks the identical-manifest invariant at every
	// prefix -- what the previous ReplayTo-per-prefix loop did -- in O(n)
	// rather than O(n^2) rebuilds. That loop made this the single slowest test
	// in the module gate; the endpoints are still rebuilt through ReplayTo so
	// the public /undo path stays exercised here, and replay/replay_test.go
	// covers ReplayTo's clamping and resume behaviour independently.
	if len(e.L.Intents) == 0 {
		t.Fatal("precondition: no recorded intents to replay")
	}
	total := len(e.L.Intents)
	for _, k := range []int{0, total / 2, total} {
		r, err := replay.ReplayTo(e.L, cfg, k)
		if err != nil {
			t.Fatalf("ReplayTo intent %d: %v", k, err)
		}
		if got := r.OwnDeck(0); !sameManifest(got, genesis) {
			t.Fatalf("replay at intent %d manifest = %#v, want %#v", k, got, genesis)
		}
	}
	if _, err := replay.Walk(e.L, cfg, total, func(r *rules.Engine, k int) error {
		if got := r.OwnDeck(0); !sameManifest(got, genesis) {
			return fmt.Errorf("replay at intent %d manifest = %#v, want %#v", k, got, genesis)
		}
		return nil
	}); err != nil {
		t.Fatalf("Walk: %v", err)
	}

	// The library the manifest describes is NOT static: the same sequence
	// shows a different library order than genesis. This is what makes the
	// static-manifest claim meaningful rather than vacuous.
	genesisLib := libraryOrder(rules.New(cfg).G, 0)
	finalLib := libraryOrder(e.G, 0)
	if slices.Equal(genesisLib, finalLib) {
		t.Fatal("precondition: library order never changed; the privacy claim is vacuous")
	}
}

func sameManifest(a *deck.Manifest, b *deck.Manifest) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Name == b.Name && slices.Equal(a.Main, b.Main) && slices.Equal(a.Sideboard, b.Sideboard) && slices.Equal(a.Commanders, b.Commanders)
}

func libraryOrder(g *state.Game, p state.PlayerID) []state.ObjID {
	return append([]state.ObjID(nil), g.Zone(state.ZLibrary, p)...)
}
