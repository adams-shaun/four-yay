package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestHeraldPreventsEmptyLibraryLossInCastDrawPaths covers both ordinary-draw
// continuations that bypass effects/cardflow.go: a draw paid for by a cast
// cost, and the ordinary draw resumed after declining dredge.
func TestHeraldPreventsEmptyLibraryLossInCastDrawPaths(t *testing.T) {
	t.Parallel()
	for i, draw := range []struct {
		name string
		call func(*Engine)
	}{
		{name: "cast draw cost", call: func(e *Engine) { e.drawCostCard(0) }},
		{name: "declined dredge", call: func(e *Engine) { e.resumeOrdinaryDraw(0) }},
	} {
		t.Run(draw.name, func(t *testing.T) {
			e, cfg, hid, _ := cantLoseFixture(t, uint64(8310+i), "")
			heraldOnBattlefield(t, e, hid)
			cantLoseEmptyLibrary(t, e, 0)
			if got := len(e.G.Zone(state.ZLibrary, 0)); got != 0 {
				t.Fatalf("precondition: library has %d cards, want empty", got)
			}

			draw.call(e)
			if e.G.Players[0].Lost {
				t.Fatal("Herald did not prevent the cast-flow empty-library loss")
			}
			for _, ev := range e.L.Events {
				if ev.Kind == events.Note && ev.Text == "unimplemented replacement GameLoss" {
					t.Fatal("GameLoss handler did not run")
				}
			}

			// Negative control: the same attempted empty-library draw must lose
			// as soon as Herald leaves and the next draw proposes the loss.
			e.emit(events.Event{Kind: events.MoveZone, Obj: hid, From: state.ZBattlefield, To: state.ZGraveyard})
			draw.call(e)
			if !e.G.Players[0].Lost {
				t.Fatal("seat 0 must lose to the empty library once Herald is gone")
			}
			replayCheck(t, e, cfg)
		})
	}
}
