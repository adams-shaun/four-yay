package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func TestExchangeLifeSettlesWhenLichReplacesGainWithDraw(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	lich := mustCorpusCard(t, reg, "Lich")
	e := newSeats(t, 2)
	source := onBoardCard(t, e, 1, lich)
	e.emit(events.Event{Kind: events.LifeChange, Player: 1, Amount: -5})
	if e.G.Players[0].Life != 20 || e.G.Players[1].Life != 15 {
		t.Fatalf("precondition: lives = %d/%d, want distinct 20/15", e.G.Players[0].Life, e.G.Players[1].Life)
	}
	ctx := &effects.Ctx{Source: source, Controller: 0, ExchangeMemory: &effects.ExchangeMemory{Bound: true}}
	handBefore := len(e.G.Zone(state.ZHand, 1))
	e.pending = nil
	e.ExchangeLife(events.Event{Kind: events.LifeChange, Player: 0, Amount: -5},
		events.Event{Kind: events.LifeChange, Player: 1, Amount: 5}, 0, 20, ctx, true)
	if e.G.Players[0].Life != 15 || e.G.Players[1].Life != 15 {
		t.Fatalf("lives after replaced side = %d/%d, want 15/15", e.G.Players[0].Life, e.G.Players[1].Life)
	}
	if got := len(e.G.Zone(state.ZHand, 1)) - handBefore; got != 5 {
		t.Fatalf("Lich replacement drew %d, want 5", got)
	}
	if ctx.ExchangeMemory.Number != 5 {
		t.Fatalf("RememberOwnLoss = %d, want actual loss 5", ctx.ExchangeMemory.Number)
	}
}

func TestExchangeLifeSettlesWhenCantGainLifeConsumesSide(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	leyline := mustCorpusCard(t, reg, "Leyline of Punishment")
	e := newSeats(t, 2)
	e.emit(events.Event{Kind: events.LifeChange, Player: 0, Amount: -5})
	e.emit(events.Event{Kind: events.LifeChange, Player: 1, Amount: 5})
	onBoardCard(t, e, 0, leyline)
	if e.G.Players[0].Life != 15 || e.G.Players[1].Life != 25 {
		t.Fatalf("precondition: lives = %d/%d, want distinct 15/25", e.G.Players[0].Life, e.G.Players[1].Life)
	}
	e.pending = nil
	e.ExchangeLife(events.Event{Kind: events.LifeChange, Player: 0, Amount: 10},
		events.Event{Kind: events.LifeChange, Player: 1, Amount: -10}, 0, 15, nil, false)
	if e.G.Players[0].Life != 15 || e.G.Players[1].Life != 15 {
		t.Fatalf("lives after prevented side = %d/%d, want 15/15", e.G.Players[0].Life, e.G.Players[1].Life)
	}
	if !hasNoteText(e, "ExchangeLife settled with a replaced life-change side") {
		t.Fatal("missing Note for exchange with a consumed life-change side")
	}
}

func TestExchangeLifeTwoTargetShapeUsesEngineTransaction(t *testing.T) {
	t.Parallel()
	e := newSeats(t, 2)
	e.emit(events.Event{Kind: events.LifeChange, Player: 0, Amount: -5})
	if e.G.Players[0].Life != 15 || e.G.Players[1].Life != 20 {
		t.Fatalf("precondition: lives = %d/%d, want distinct 15/20", e.G.Players[0].Life, e.G.Players[1].Life)
	}
	e.pending = nil
	e.ExchangeLife(events.Event{Kind: events.LifeChange, Player: 0, Amount: 5},
		events.Event{Kind: events.LifeChange, Player: 1, Amount: -5}, 0, 15, nil, false)
	if e.G.Players[0].Life != 20 || e.G.Players[1].Life != 15 {
		t.Fatalf("two-target exchange = %d/%d, want 20/15", e.G.Players[0].Life, e.G.Players[1].Life)
	}
}

func hasNoteText(e *Engine, text string) bool {
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && ev.Text == text {
			return true
		}
	}
	return false
}
