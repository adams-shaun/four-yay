package rules

// CR 603.10a: a leaves-the-battlefield trigger looks back in time, so a
// permanent that leaves simultaneously with others sees them leave. An
// effect's departure batch (DestroyAll, a multi-permanent Sacrifice, a
// ChangeZoneAll sweep) is matched against ONE pre-batch board, parked by
// Engine.BatchDepartures -- the state-based batches' discipline. Every card
// is a REAL corpus card.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// lookBackResolve casts seat 0's first castable spell and drives the game
// until the stack is empty, answering every target ask with its first option
// (the drain triggers' "target opponent" in a two-player game) and every
// trigger-order ask in the offered order.
func lookBackResolve(t *testing.T, e *Engine) {
	t.Helper()
	castFirst(t, e, "cast")
	for i := 0; i < 200 && !e.G.Over; i++ {
		d := e.Pending()
		if d == nil {
			t.Fatal("no decision pending while resolving")
		}
		switch d.Kind {
		case decision.KPriority:
			if len(e.G.Stack) == 0 {
				return
			}
			pass := -1
			for _, o := range d.Options {
				if o.Kind == "pass" {
					pass = o.Index
				}
			}
			submitChoices(t, e, pass)
		case decision.KTriggerOrder:
			order := make([]int, len(d.Options))
			for j := range order {
				order[j] = j
			}
			submitChoices(t, e, order...)
		default:
			submitChoices(t, e, 0)
		}
	}
	t.Fatal("the stack did not empty")
}

func TestSimultaneousDeathsAreSeenByADyingWatcher(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	cases := []struct {
		name string
		// battlefield is seat 0's board, in entry order; the watcher's place
		// in it must not matter.
		battlefield []string
		spell, mana string
		// drains is how many times a watcher's drain resolves: each is one
		// life from seat 1 to seat 0.
		drains int32
	}{
		{"destroy all, watcher first", []string{"Vengeful Bloodwitch", "Grizzly Bears"}, "Day of Judgment", "CCWW", 2},
		{"destroy all, watcher last", []string{"Grizzly Bears", "Vengeful Bloodwitch"}, "Day of Judgment", "CCWW", 2},
		// Two watchers and a third creature: each watcher sees all three die.
		{"destroy all, two watchers", []string{"Vengeful Bloodwitch", "Grizzly Bears", "Vengeful Bloodwitch"}, "Day of Judgment", "CCWW", 6},
		// The same batch through the Sacrifice effect (each player
		// sacrifices two creatures): both of seat 0's creatures go at once.
		{"sacrifice batch", []string{"Vengeful Bloodwitch", "Grizzly Bears"}, "Barter in Blood", "CCBB", 2},
		// Control: the watcher dying alone triggers once.
		{"watcher alone", []string{"Vengeful Bloodwitch"}, "Day of Judgment", "CCWW", 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			deck := []*cards.Card{lookup(t, reg, tc.spell)}
			for _, n := range tc.battlefield {
				deck = append(deck, lookup(t, reg, n))
			}
			e, cfg := corpusEngineCfg(t, reg, deck, nil)
			var ids []state.ObjID
			for _, n := range tc.battlefield {
				ids = append(ids, moveCorpusCard(t, e, n, 0, state.ZBattlefield))
			}
			moveCorpusCard(t, e, tc.spell, 0, state.ZHand)
			life0, life1 := e.G.Players[0].Life, e.G.Players[1].Life
			addMana(t, e, 0, tc.mana)
			lookBackResolve(t, e)
			for _, id := range ids {
				if z := e.G.Obj(id).Zone; z != state.ZGraveyard {
					t.Fatalf("%s is in %s, want the graveyard", e.G.Obj(id).Face().Name, z)
				}
			}
			if g0, g1 := e.G.Players[0].Life-life0, life1-e.G.Players[1].Life; g0 != tc.drains || g1 != tc.drains {
				t.Errorf("seat 0 gained %d and seat 1 lost %d, want %d each (one per death each watcher saw)", g0, g1, tc.drains)
			}
			if e.triggerBefore != nil || e.batchWindow != nil || e.batchWindowDepth != 0 {
				t.Errorf("a look-back window outlived the batch: triggerBefore=%v batchWindow=%v depth=%d",
					e.triggerBefore != nil, e.batchWindow != nil, e.batchWindowDepth)
			}
			replayCheck(t, e, cfg)
		})
	}
}
