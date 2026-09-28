package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestManaClashRepeatEachSharesFlipMemory drives Mana Clash's real chain:
// RepeatEach flips once for its target and once for its controller, then its
// outer SubAbility reads ValidPlayers$ FlippedTails. The per-iteration Ctx
// copies must share their lazily allocated flip memory with that outer reader.
func TestManaClashRepeatEachSharesFlipMemory(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	clash := lookup(t, reg, "Mana Clash")

	for seed := uint64(1); seed <= 32; seed++ {
		e, _ := flipEngine(t, reg, seed, []*cards.Card{clash}, nil)
		id := moveByName(t, e, 0, "Mana Clash", state.ZHand)
		if got := e.G.Obj(id).Face().Name; got != "Mana Clash" {
			t.Fatalf("precondition: fixture card = %q, want Mana Clash", got)
		}
		addMana(t, e, 0, "R")
		d := e.Pending()
		cast := -1
		for _, o := range d.Options {
			if o.Kind == "cast" && o.Obj == id {
				cast = o.Index
			}
		}
		if cast < 0 {
			t.Fatalf("seed %d precondition: Mana Clash was not castable: %+v", seed, d.Options)
		}
		submitChoices(t, e, cast)
		d = e.Pending()
		if d == nil || d.Kind != decision.KTarget {
			t.Fatalf("seed %d precondition: Mana Clash target ask = %+v, want a player target ask", seed, d)
		}
		submitTargetOnSeat(t, e, 1)
		passUntilStackEmpty(t, e, 200)

		flips := flipByPlayer(e)
		if len(flips) < 2 {
			t.Fatalf("seed %d precondition: Mana Clash RepeatEach made %d flips, want at least target and controller", seed, len(flips))
		}
		if flips[0].Heads && flips[1].Heads {
			continue // this round has no FlippedTails reader input
		}
		var hits int
		for _, ev := range e.L.Events {
			if ev.Kind == events.Damage && ev.Amount == 1 {
				hits++
			}
		}
		if hits == 0 {
			t.Fatalf("seed %d: Mana Clash flips %v include a first-round tail, but its post-RepeatEach FlippedTails reader dealt no damage", seed, flips)
		}
		return
	}
	t.Fatal("precondition: seeds 1..32 produced no Mana Clash round with a tail")
}
