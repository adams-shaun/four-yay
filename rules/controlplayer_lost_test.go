package rules

// CR 723 (called "CR 720" throughout this package's existing comments)
// state-hygiene regression: a player-controlling effect ends when either
// participant leaves the game (CR 723.4/723.5). The loss fold must clear both
// directions of state.Game.ControlledBy/ControlArmedTurn -- the lost seat as
// the controlled player AND the lost seat as the controller -- or a still-
// active controlled seat's decisions keep being rewritten to a departed
// controller (rules/engine.go controlPlayerRedirect has no Lost check of its
// own; the fold is where a departure is observed).

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// grantControlPlayer runs the fixture's ControlPlayer ability through to
// resolution and asserts the grant landed (seat 1 controlled by seat 0), the
// same precondition every case below depends on.
func grantControlPlayer(t *testing.T, e *Engine, id state.ObjID) {
	t.Helper()
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZBattlefield})
	if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: fixture on battlefield, got %+v", o)
	}
	addMana(t, e, 0, "CCCC")
	e.Advance()
	opt := abilityOption(t, e, id, 0)
	submitChoices(t, e, opt.Index)
	d := passToAsk(t, e, 10)
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("no target ask after activating the ability (got %+v)", d)
	}
	idx := indexOfPlayerOption(d, 1)
	if idx < 0 {
		t.Fatalf("target ask offered no opponent: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
		t.Fatalf("target opponent: %v", err)
	}
	passUntilStackEmpty(t, e, 20)
	if ctl, ok := e.G.ControlledBy[1]; !ok || ctl != 0 {
		t.Fatalf("precondition: grant did not land, ControlledBy = %+v", e.G.ControlledBy)
	}
	// The API ran, not an "unimplemented API ControlPlayer" Note -- a
	// "nothing was granted" pass would otherwise satisfy the assertion above
	// for the wrong reason if the fold were absent.
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "unimplemented API ControlPlayer") {
			t.Fatalf("api:ControlPlayer is not registered: %q", ev.Text)
		}
	}
}

// TestControlPlayerControlledSeatLosesClearsFold is the direction the
// findings-t2 report named: when the CONTROLLED player loses, its own fold
// entries are removed.
func TestControlPlayerControlledSeatLosesClearsFold(t *testing.T) {
	t.Parallel()
	e, cfg, id := newFixtureDeck(t, 12, controlPlayerFixture)
	grantControlPlayer(t, e, id)
	// Precondition: both maps carry seat 1 now, or there is nothing to clear
	// and the assertions below pass vacuously.
	if e.G.ControlledBy[1] != 0 || e.G.ControlArmedTurn[1] == 0 {
		t.Fatalf("precondition: fold entries for seat 1 = (%d, %d)",
			e.G.ControlledBy[1], e.G.ControlArmedTurn[1])
	}

	e.emit(events.Event{Kind: events.PlayerLost, Player: 1, Text: "test controlled departure"})
	if !e.G.Players[1].Lost {
		t.Fatal("precondition: PlayerLost did not mark seat 1 departed")
	}
	if ctl, ok := e.G.ControlledBy[1]; ok {
		t.Fatalf("lost controlled seat 1 still folded (controller %d): %+v", ctl, e.G.ControlledBy)
	}
	if _, ok := e.G.ControlArmedTurn[1]; ok {
		t.Fatalf("lost controlled seat 1 still armed: %+v", e.G.ControlArmedTurn)
	}
	replayCheck(t, e, cfg)
}

// TestControlPlayerControllerLosesClearsFoldAndRedirect is the more serious
// direction the report misses. Seat 0 controls seat 1; seat 0 leaves during
// seat 1's controlled turn. Without the fold clear, ControlledBy[1] survives
// and seat 1's every decision is rewritten to the departed seat 0.
func TestControlPlayerControllerLosesClearsFoldAndRedirect(t *testing.T) {
	t.Parallel()
	e, cfg, id := newFixtureDeck(t, 13, controlPlayerFixture)
	grantControlPlayer(t, e, id)

	// Drive into seat 1's controlled turn. The redirect is live only on the
	// controlled seat's own next turn (CR 720.2), so the precondition below
	// is exactly the state the redirect requires.
	driveToTurn(t, e, 2, 1)
	if e.G.Active != 1 {
		t.Fatalf("precondition: seat 1's turn, active = %d", e.G.Active)
	}
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("no priority at seat 1's Main1 (got %+v)", d)
	}
	if d.Player != 0 {
		t.Fatalf("precondition: redirect not live, seat 1's priority posed to %d, want controller 0", d.Player)
	}

	// The controller leaves during the controlled turn.
	e.emit(events.Event{Kind: events.PlayerLost, Player: 0, Text: "test controller departure"})
	if !e.G.Players[0].Lost {
		t.Fatal("precondition: PlayerLost did not mark the controller departed")
	}
	// The fold cleared the entry naming seat 0 as controller.
	if ctl, ok := e.G.ControlledBy[1]; ok {
		t.Fatalf("lost controller 0 still folded for seat 1: ctl=%d map=%+v", ctl, e.G.ControlledBy)
	}
	if _, ok := e.G.ControlArmedTurn[1]; ok {
		t.Fatalf("lost controller 0 still armed for seat 1: %+v", e.G.ControlArmedTurn)
	}

	// And the NEXT decision seat 1 is offered is posed to seat 1 itself, not
	// the departed controller. Advance past the already-projected pending
	// priority, then read the next ask: it must not name seat 0.
	if err := submitPassControl(t, e); err != nil {
		t.Fatalf("pass the stale pending priority: %v", err)
	}
	for i := 0; i < 20; i++ {
		nd := e.Pending()
		if nd == nil {
			break
		}
		if nd.Player == 0 {
			t.Fatalf("seat 1's decision still posed to departed controller 0: kind=%v", nd.Kind)
		}
		if nd.Kind == decision.KPriority {
			break
		}
		if len(nd.Options) == 0 {
			break
		}
		if err := e.Submit(decision.Intent{Seq: nd.Seq, Player: nd.Player, Choices: []int{nd.Options[0].Index}}); err != nil {
			t.Fatalf("advance: %v", err)
		}
	}
	replayCheck(t, e, cfg)
}

// submitPass submits the pending decision's "pass" option when it is a
// priority decision, and its first option otherwise.
func submitPassControl(t *testing.T, e *Engine) error {
	t.Helper()
	d := e.Pending()
	if d == nil {
		return nil
	}
	if d.Kind == decision.KPriority {
		for _, o := range d.Options {
			if o.Kind == "pass" {
				return e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{o.Index}})
			}
		}
	}
	if len(d.Options) == 0 {
		return nil
	}
	return e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{d.Options[0].Index}})
}
