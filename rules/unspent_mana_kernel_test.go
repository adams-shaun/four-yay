package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// Leyline Tyrant's death trigger pays X from the banked red pool under the
// kernel: the X window is bounded by the bank, no mana-activation ask, the
// reflexive damage hits the chosen target.

func TestLeylineTyrantDeathTriggerSpendsTheBankKernel(t *testing.T) {
	t.Parallel()
	e := newSeats(t, 2)
	tyrant := onBoardCard(t, e, 0, corpusAlternativeCard(t, "Leyline Tyrant"))
	if life := e.G.Players[1].Life; life != 20 {
		t.Fatalf("test precondition: opponent life = %d, want 20", life)
	}
	if pool := bankAtEndStep(t, e, state.Mana{state.MR: 2}); pool[state.MR] != 2 {
		t.Fatalf("test precondition: the boundary did not bank the red: %+v", pool)
	}
	// Replay anchor: the kill, the pay window, the payment and the damage are
	// all event-sourced from here.
	replayed := e.G.Clone()
	start := len(e.L.Events)

	// Kill it; the death trigger enters the pay window.
	e.emit(events.Event{Kind: events.MoveZone, Obj: tyrant, From: state.ZBattlefield, To: state.ZGraveyard})
	e.putTriggersOnStack()
	if len(e.G.Stack) == 0 {
		t.Fatal("the death trigger never reached the stack")
	}
	kr9ResolveTop(e)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || len(d.Options) == 0 || d.Options[0].Kind != "trigger_cost_x" {
		t.Fatalf("expected the choose-X announcement ask, got %+v", d)
	}
	if d.Options[0].Amount != 0 {
		t.Fatalf("choose-X options must ascend from 0, first is %d", d.Options[0].Amount)
	}
	if last := d.Options[len(d.Options)-1]; last.Amount != 2 {
		t.Fatalf("choose-X bound = %d, want 2 (the banked pool is the whole potential)", last.Amount)
	}
	submitChoices(t, e, xFoldAskIndex(t, d, 2))

	// The payment ask: the banked pool covers X = 2 whole, so no
	// mana-activation ask may appear.
	d = e.Pending()
	if d == nil {
		t.Fatal("no payment ask pending after the X announcement")
	}
	pay := -1
	for _, o := range d.Options {
		if o.Kind == "activate" {
			t.Fatalf("the window asked to activate mana (%+v): the banked pool should cover X = 2", d.Options)
		}
		if o.Kind == "trigger_cost_pay" {
			pay = o.Index
		}
	}
	if pay < 0 {
		t.Fatalf("no pay option in the payment ask: %+v", d.Options)
	}
	submitChoices(t, e, pay)

	// CR 603.12: "When you do" is a reflexive triggered ability, so its "any
	// target" is a placement target ask as it goes on the stack; take the
	// opponent. (The probe's answering Submit re-ran only the resolution;
	// the next priority round stacks the reflexive trigger.)
	kr9Settle(e)
	d = e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected the reflexive ability's damage target ask, got %+v", d)
	}
	tgt := -1
	for _, o := range d.Options {
		if o.Kind == "player" && o.Player == 1 {
			tgt = o.Index
		}
	}
	if tgt < 0 {
		t.Fatalf("the opponent was not offered as an Any target: %+v", d.Options)
	}
	submitChoices(t, e, tgt)
	if life := e.G.Players[1].Life; life != 20 {
		t.Fatalf("opponent life = %d before the reflexive ability resolved, want 20", life)
	}
	passUntilStackEmpty(t, e, 20)

	if life := e.G.Players[1].Life; life != 18 {
		t.Fatalf("opponent life = %d after paying X = 2, want 18", life)
	}
	if pool := e.G.Players[0].Pool; pool[state.MR] != 0 {
		t.Fatalf("red pool = %d after paying X = 2, want 0 (the bank paid it)", pool[state.MR])
	}
	kr9ReplaySince(t, e, replayed, start)
}

// kr9ReplaySince is replaySince compared with diffGames (the replayCheck
// comparison): a kernel re-run restores the checkpoint snapshot, whose empty
// zone slices are non-nil where the live-applied ones were nil, a
// representation difference reflect.DeepEqual reports and the game never
// reads.
func kr9ReplaySince(t *testing.T, e *Engine, replayed *state.Game, start int) {
	t.Helper()
	for _, ev := range e.L.Events[start:] {
		events.Apply(replayed, ev)
	}
	if d := diffGames(replayed, e.G); d != "" {
		t.Fatalf("the events since the anchor do not replay the live game:\n%s", d)
	}
}
