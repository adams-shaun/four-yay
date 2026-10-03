// Fix round (findings-sol4 MAJOR): a GainLife→Draw replacement body (Lich's
// "If you would gain life, draw that many cards instead") drives its draws
// through effects.DrawFor, and each draw may suspend on a Dredge ask (CR
// 702.55). The old `case "Draw"` loop did not check h.Suspended() between
// iterations: the first draw's ask was orphaned by the second draw's own ask
// (two DecisionAsk events for one life event), and answering the surviving
// ask produced one draw where the card said two.
//
// The committed probe is exactly the finding's repro, on real card scripts:
// Lich on the battlefield, a Golgari Thug (Dredge 4) in its controller's
// graveyard, one LifeChange{Amount: 2} emit. Post-fix the loop parks the
// remaining count on the ask's resume point, the answered dredge re-drives
// the rest, and a second dredger re-parks for its own sequential ask.
package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// lichThugEngine deals seat 0 a deck led by the named cards, moves every
// named card out of seat 0's zones (thugs to the graveyard, the Lich onto
// the battlefield), and returns the engine. The tests use NEFARIOUS Lich
// rather than Lich because its GainLife replacement is the same shape
// ("If you would gain life, draw that many cards instead") without Lich's
// entry life drain, which would park seat 0 at 0 life — and the engine does
// not implement the R:Event$ GameLoss CantHappen family that keeps Lich's
// controller alive there (recorded as an issue, not fixed here).
func lichThugEngine(t *testing.T, seed uint64, lead ...string) (*Engine, Config, []state.ObjID) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	deck := []*cards.Card{}
	for _, name := range lead {
		c, ok := reg.Lookup(name)
		if !ok {
			t.Fatalf("%s missing from corpus", name)
		}
		if d := c.Link(); len(d) != 0 {
			t.Fatalf("link %s: %v", name, d)
		}
		deck = append(deck, c)
	}
	deck = append(deck, mountainDeck(t, 40-len(lead))...)
	cfg := seatZeroStart(Config{Seed: seed, Names: []string{"a", "b"},
		Decks:  [][]*cards.Card{deck, mountainDeck(t, 40)},
		Tokens: map[string]*cards.Card{}})
	e := New(cfg)
	e.Advance()
	ids := make([]state.ObjID, 0, len(lead))
	for _, name := range lead {
		var tid state.ObjID
		for _, z := range []state.Zone{state.ZHand, state.ZLibrary} {
			for _, id := range e.G.Zone(z, 0) {
				if o := e.G.Obj(id); o.Face() != nil && o.Face().Name == name {
					tid = id
				}
			}
		}
		if tid == 0 {
			t.Fatalf("%s was not dealt", name)
		}
		to := state.ZGraveyard
		if name == "Nefarious Lich" {
			to = state.ZBattlefield
		}
		e.emit(events.Event{Kind: events.MoveZone, Obj: tid, From: e.G.Obj(tid).Zone, To: to})
		ids = append(ids, tid)
	}
	return e, cfg, ids
}

// countAsks counts the mid-resolution KModes (dredge) DecisionAsk events
// for seat 0 since `since` — not the turn-structure priority asks that the
// surrounding Submit tail legitimately grants once the body completes.
func countAsks(t *testing.T, e *Engine, since int) int {
	t.Helper()
	n := 0
	for _, ev := range e.L.Events[since:] {
		if ev.Kind == events.DecisionAsk && ev.Player == 0 && ev.Text == string(decision.KModes) {
			n++
		}
	}
	return n
}
