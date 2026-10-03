package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// TestCloneCarriesParkedLifeExchange pins that a clone taken while an
// ExchangeLife transaction is parked (a side's life change suspended on a
// decision, so the transaction is live at that intent boundary) carries its
// own copy: settling the clone's transaction completes the exchange on the
// clone alone, and the original keeps its transaction parked.
func TestCloneCarriesParkedLifeExchange(t *testing.T) {
	names, decks := testutil.SampleDecks(t, 2)
	e := New(Config{Seed: 3, Names: names, Decks: decks})
	e.Advance()
	drive(t, e, newTestBot(3), 10)
	life := e.G.Players[0].Life
	// A one-sided ExchangeLifeVariant whose life change already landed: the
	// settle sets the source's P/T (a continuous effect).
	e.pendingLifeExchange = &lifeExchangeTransaction{source: 1, controller: 0, player: 0,
		lifeBefore: life + 3, oldLife: life + 3, setPower: true,
		staged: []events.Event{{Kind: events.LifeChange, Player: 0}}}

	c := e.Clone()
	if c.pendingLifeExchange == nil {
		t.Fatal("clone dropped the parked life-exchange transaction")
	}
	if c.pendingLifeExchange == e.pendingLifeExchange {
		t.Fatal("clone shares the parked life-exchange transaction with the original")
	}
	c.pendingLifeExchange.staged[0].Player = 1
	if e.pendingLifeExchange.staged[0].Player != 0 {
		t.Fatal("clone shares the parked transaction's staged sides")
	}

	before, origBefore := len(c.continuous), len(e.continuous)
	c.pending = nil // the decision the side suspended on has been answered
	c.settlePendingLifeExchange()
	if c.pendingLifeExchange != nil || len(c.continuous) != before+1 {
		t.Fatalf("settling the clone's exchange did not complete it: parked %v, continuous %d -> %d",
			c.pendingLifeExchange != nil, before, len(c.continuous))
	}
	if e.pendingLifeExchange == nil || len(e.continuous) != origBefore {
		t.Fatal("settling the clone's exchange touched the original")
	}
}
