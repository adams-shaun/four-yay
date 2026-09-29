package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

const redistributeSA = "SP$ SetLife | PlayerChoices$ Player | ChoiceAmount$ Any | ChoicePrompt$ Choose any number of players | Redistribute$ True"

// The scripted host mirrors the engine's choice resume, including the
// accumulated choices and cursor that survive across successive asks.
func redistributeAnswer(t *testing.T, h *chooseNumberHost, c *Ctx, ability string, picks ...int) {
	t.Helper()
	if len(h.asks) == 0 {
		t.Fatal("SetLife posed no redistribution ask (handler not reached)")
	}
	d := h.asks[len(h.asks)-1]
	if err := d.Validate(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: picks}); err != nil {
		t.Fatalf("invalid answer: %v", err)
	}
	next := &Ctx{Controller: c.Controller, Source: c.Source, ChoiceTarget: d.ResumeTarget, Chosen: append([]state.Target(nil), d.ResumeChoices...), ChosenValid: d.ResumeChosenValid, ChoiceDone: true}
	for _, j := range picks {
		o := d.Options[j]
		next.Choice = append(next.Choice, state.Target{Player: o.Player, IsPlayer: true})
	}
	h.suspended = false
	Resolve(h, next, sa(t, ability))
}

func TestSetLifeRedistributePermutation(t *testing.T) {
	h := &chooseNumberHost{}
	h.g = state.NewGame(names(3))
	h.g.Players[0].Life, h.g.Players[1].Life, h.g.Players[2].Life = 20, 5, 2
	if h.g.Players[0].Life == h.g.Players[2].Life || h.g.Players[1].Life == h.g.Players[2].Life {
		t.Fatal("precondition: three distinct life totals required")
	}
	c := &Ctx{Controller: 0}
	Resolve(h, c, sa(t, redistributeSA))
	if len(h.asks) != 1 {
		t.Fatalf("subset asks = %d, want 1", len(h.asks))
	}
	d := h.asks[0]
	if d.Kind != decision.KChoose || d.Player != 0 || d.Min != 0 || d.Max != 3 || d.ResumeKind != "choice" || d.ResumeTarget != 0 || d.Prompt != "Choose any number of players" || len(d.Options) != 3 {
		t.Fatalf("subset ask: %+v", d)
	}
	for i, o := range d.Options {
		if o.Kind != "player" || o.Player != state.PlayerID(i) {
			t.Fatalf("subset option %d: %+v", i, o)
		}
	}
	// Subset seats 0 and 2: seat 1 must remain untouched. Assign seat 2's
	// original 2 to seat 0, then seat 0's original 20 to seat 2.
	redistributeAnswer(t, h, c, redistributeSA, 0, 2)
	d = h.asks[len(h.asks)-1]
	if d.Kind != decision.KChoose || d.ResumeKind != "choice" || d.ResumeTarget != 1 || d.Min != 1 || d.Max != 1 || len(d.Options) != 2 || d.Options[0].Player != 0 || d.Options[1].Player != 2 {
		t.Fatalf("first assignment: %+v", d)
	}
	redistributeAnswer(t, h, c, redistributeSA, 1)
	d = h.asks[len(h.asks)-1]
	if d.ResumeTarget != 2 || len(d.Options) != 1 || d.Options[0].Player != 0 {
		t.Fatalf("remaining assignment: %+v", d)
	}
	if h.g.Players[0].Life != 20 || h.g.Players[2].Life != 2 {
		t.Fatalf("life changed before all answers: %+v", h.g.Players)
	}
	redistributeAnswer(t, h, c, redistributeSA, 0)
	if h.g.Players[0].Life != 2 || h.g.Players[1].Life != 5 || h.g.Players[2].Life != 20 {
		t.Fatalf("redistributed lives = %+v", h.g.Players)
	}
	var changes []events.Event
	for _, ev := range h.log {
		if ev.Kind == events.LifeChange {
			changes = append(changes, ev)
		}
	}
	if len(changes) != 2 || changes[0].Player != 0 || changes[0].Amount != -18 || changes[1].Player != 2 || changes[1].Amount != 18 {
		t.Fatalf("life deltas = %+v", changes)
	}
}

func TestSetLifeRedistributeZeroAndOne(t *testing.T) {
	for _, pick := range [][]int{nil, {1}} {
		h := &chooseNumberHost{}
		h.g = state.NewGame(names(3))
		h.g.Players[0].Life, h.g.Players[1].Life, h.g.Players[2].Life = 20, 5, 2
		if h.g.Players[1].Life == h.g.Players[2].Life {
			t.Fatal("precondition: player totals must differ")
		}
		c := &Ctx{Controller: 0}
		Resolve(h, c, sa(t, redistributeSA))
		redistributeAnswer(t, h, c, redistributeSA, pick...)
		if len(pick) == 1 {
			d := h.asks[len(h.asks)-1]
			if len(d.Options) != 1 || d.Options[0].Player != 1 {
				t.Fatalf("one-player assignment = %+v", d)
			}
			redistributeAnswer(t, h, c, redistributeSA, 0)
		}
		if len(h.asks) != 1+len(pick) || h.g.Players[0].Life != 20 || h.g.Players[1].Life != 5 || h.g.Players[2].Life != 2 {
			t.Fatalf("zero/one selection: asks=%d players=%+v", len(h.asks), h.g.Players)
		}
		for _, ev := range h.log {
			if ev.Kind == events.Note || ev.Kind == events.LifeChange {
				t.Fatalf("unexpected event: %+v", ev)
			}
		}
	}
}

func TestSetLifeRedistributeNoHost(t *testing.T) {
	h := newHost(t, 3)
	h.g.Players[0].Life, h.g.Players[1].Life, h.g.Players[2].Life = 20, 5, 2
	if h.g.Players[0].Life == h.g.Players[1].Life {
		t.Fatal("precondition: distinct totals required")
	}
	Resolve(h, &Ctx{Controller: 0}, sa(t, redistributeSA))
	if h.askCount != 1 {
		t.Fatalf("SetLife handler did not ask: ask count=%d", h.askCount)
	}
	if h.g.Players[0].Life != 20 || h.g.Players[1].Life != 5 || h.g.Players[2].Life != 2 {
		t.Fatalf("no-host changed life: %+v", h.g.Players)
	}
	for _, ev := range h.log {
		if ev.Kind == events.Note || ev.Kind == events.LifeChange {
			t.Fatalf("unexpected no-host event: %+v", ev)
		}
	}
}
