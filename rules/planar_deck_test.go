package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

func TestPlanarDeckGenesisAndWalkReplay(t *testing.T) {
	reg := searchTestRegistry(t)
	planeA := searchCorpusCard(t, reg, "Aretopolis")
	planeB := searchCorpusCard(t, reg, "Pools of Becoming")
	forest := searchCorpusCard(t, reg, "Forest")
	deck := make([]*cards.Card, 7)
	for i := range deck {
		deck[i] = forest
	}
	cfg := Config{Seed: 1901, Names: []string{"A", "B"}, Decks: [][]*cards.Card{deck, deck},
		PlanarDecks: [][]*cards.Card{{planeA, planeB}}}
	e := New(cfg)
	ids := e.G.Zone(state.ZPlanarDeck, 0)
	if len(ids) != 2 {
		t.Fatalf("planar deck has %d cards, want 2", len(ids))
	}
	first, second := e.G.Obj(ids[0]), e.G.Obj(ids[1])
	if first == nil || second == nil || first.FaceDown || !second.FaceDown || first.Face() == nil || second.Face() == nil || first.Face().Name == second.Face().Name {
		t.Fatalf("genesis did not reveal a distinct top plane: order=%v first=%+v second=%+v", ids, first, second)
	}
	var shuffle *events.Event
	var reveal bool
	for i := range e.L.Events {
		ev := &e.L.Events[i]
		if ev.Kind == events.PlanarDeckShuffle && ev.Secret {
			shuffle = ev
		}
		reveal = reveal || ev.Kind == events.PlanarReveal && ev.Obj == ids[0] && !ev.Secret
	}
	if shuffle == nil || len(shuffle.IDs) != 2 || !reveal {
		t.Fatalf("genesis events missing planar shuffle/reveal: shuffle=%+v reveal=%v", shuffle, reveal)
	}
	if e.G.Obj(shuffle.IDs[0]).Face().Name == e.G.Obj(shuffle.IDs[1]).Face().Name {
		t.Fatalf("privacy fixture needs distinguishable planes, shuffle IDs=%v", shuffle.IDs)
	}
	for _, viewer := range []state.PlayerID{0, 1} {
		redacted := view.RedactEvent(e.G, *shuffle, viewer)
		if len(redacted.IDs) != 0 || redacted.Obj != 0 || !redacted.Secret || redacted.Kind != events.PlanarDeckShuffle {
			t.Fatalf("viewer %d learned private planar shuffle order: %+v", viewer, redacted)
		}
	}
	omniscient := view.RedactEventFor(e.G, *shuffle, 0, view.Omniscient)
	if len(omniscient.IDs) != 0 || omniscient.Obj != 0 || !omniscient.Secret || omniscient.Kind != events.PlanarDeckShuffle {
		t.Fatalf("omniscient spectator learned private planar shuffle order: %+v", omniscient)
	}
	sameSeed := New(cfg).G.Zone(state.ZPlanarDeck, 0)
	if len(sameSeed) != len(ids) || sameSeed[0] != ids[0] || sameSeed[1] != ids[1] {
		t.Fatalf("same seed produced planar order %v, want %v", sameSeed, ids)
	}
	for _, viewer := range []state.PlayerID{0, 1} {
		projected := view.Project(e.G, e, viewer, nil)
		deckView := projected.Players[0].PlanarDeck
		if len(deckView) != 2 || deckView[0].Name == "" || !deckView[1].FaceDown || deckView[1].Name != "" {
			t.Fatalf("viewer %d planar deck projection leaked or hid current plane: %+v", viewer, deckView)
		}
	}
	clone := e.G.Clone()
	if got := clone.Zone(state.ZPlanarDeck, 0); len(got) != 2 || got[0] != ids[0] || got[1] != ids[1] {
		t.Fatalf("clone lost planar zone order: %v", got)
	}
	e.Planeswalk(0)
	walked := e.G.Zone(state.ZPlanarDeck, 0)
	if len(walked) != 2 || walked[0] != ids[1] || walked[1] != ids[0] || !e.G.Obj(ids[0]).FaceDown || e.G.Obj(ids[1]).FaceDown {
		t.Fatalf("planeswalk result: ids=%v face-down=%v/%v", walked, e.G.Obj(ids[0]).FaceDown, e.G.Obj(ids[1]).FaceDown)
	}
	if got := clone.Zone(state.ZPlanarDeck, 0); got[0] != ids[0] || !clone.Obj(ids[1]).FaceDown {
		t.Fatalf("walk mutated cloned game: zone=%v face-down=%v", got, clone.Obj(ids[1]).FaceDown)
	}
	replayCheck(t, e, cfg)
}
