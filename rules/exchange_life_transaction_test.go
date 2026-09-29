package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
)

func TestExchangeLifeWaitsForReplacementBeforeApplyingEitherSide(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	angel := mustCorpusCard(t, reg, "Angel of Vitality")
	boon := mustCorpusCard(t, reg, "Boon Reflection")
	e := newSeats(t, 2)
	onBoardCard(t, e, 1, angel)
	first := onBoardCard(t, e, 1, boon)
	e.emit(events.Event{Kind: events.LifeChange, Player: 1, Amount: -5})
	if e.G.Players[0].Life != 20 || e.G.Players[1].Life != 15 {
		t.Fatalf("precondition: life totals = %d/%d, want distinct 20/15", e.G.Players[0].Life, e.G.Players[1].Life)
	}
	ctx := &effects.Ctx{Source: first, Controller: 0, ExchangeMemory: &effects.ExchangeMemory{Bound: true}}
	// This direct host-level transaction has no priority window to preserve.
	e.pending = nil
	e.ExchangeLife(events.Event{Kind: events.LifeChange, Player: 0, Amount: -5},
		events.Event{Kind: events.LifeChange, Player: 1, Amount: 5}, 0, 20, ctx, true)
	d := e.Pending()
	if d == nil || d.Kind != decision.KReplacement || d.Player != 1 {
		t.Fatalf("pending = %+v, want seat 1's life-replacement choice", d)
	}
	if e.G.Players[0].Life != 20 || e.G.Players[1].Life != 15 {
		t.Fatalf("half-exchange while replacement is pending: lives %d/%d", e.G.Players[0].Life, e.G.Players[1].Life)
	}
	if ctx.ExchangeMemory.Number != 0 {
		t.Fatalf("RememberOwnLoss rider ran before exchange settled: %d", ctx.ExchangeMemory.Number)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{optionFor(t, d, first)}}); err != nil {
		t.Fatal(err)
	}
	if e.G.Players[0].Life != 15 || e.G.Players[1].Life != 26 {
		t.Fatalf("settled exchange lives = %d/%d, want 15/26", e.G.Players[0].Life, e.G.Players[1].Life)
	}
	if ctx.ExchangeMemory.Number != 5 {
		t.Fatalf("RememberOwnLoss = %d, want actual 5 life lost", ctx.ExchangeMemory.Number)
	}
}
