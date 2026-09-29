package rules

// CR 720.2 interval conformance for `api:ControlPlayer`: the controller
// answers only decisions the controlled player would be offered DURING that
// player's next turn. A decision offered to the controlled seat BEFORE their
// controlled turn begins -- most visibly the response priority the non-active
// seat gets on the controller's own turn the grant resolved on -- must reach
// the controlled seat itself, never the controller.
//
// findings-t2 MAJOR: controlPlayerRedirect read only ControlledBy and had no
// turn gating, so the controlled seat's on-controller-turn response priority
// was posed to the controller before their controlled turn. The redirect is
// now scoped by the same Turn > armed test expirePlayerControl uses.

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func TestControlPlayerRedirectDoesNotSpillBeforeControlledTurn(t *testing.T) {
	t.Parallel()
	e, cfg, id := newFixtureDeck(t, 11, controlPlayerFixture)
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

	// Precondition: the grant resolved on seat 0's own current turn -- seat 1
	// is controlled by seat 0, and the arm turn equals the current turn, so
	// seat 1's controlled turn has NOT begun.
	if ctl := e.G.ControlledBy[1]; ctl != 0 {
		t.Fatalf("precondition: ControlledBy[1] = %d, want controller 0 (%+v)", ctl, e.G.ControlledBy)
	}
	if e.G.Active != 0 || e.G.Turn != e.G.ControlArmedTurn[1] {
		t.Fatalf("precondition: active=%d turn=%d armed=%d, want seat 0 on the arm turn",
			e.G.Active, e.G.Turn, e.G.ControlArmedTurn[1])
	}

	// Drive toward the moment the ENGINE hands priority to seat 1 on this arm
	// turn. e.G.Priority is authoritative about whose priority it is; the
	// redirect rewrites only the pending decision's Player, so a pending
	// priority with e.G.Priority == 1 and Player == 0 is exactly the spill.
	found := false
	for i := 0; i < 60 && e.G.Active == 0 && e.G.Turn == e.G.ControlArmedTurn[1]; i++ {
		d = e.Pending()
		if d == nil {
			t.Fatalf("no decision pending while searching for seat 1's pre-turn priority")
		}
		if d.Kind != decision.KPriority {
			t.Fatalf("unexpected decision %s before seat 1's pre-turn priority", d.Kind)
		}
		if e.G.Priority == 1 {
			found = true
			break
		}
		pass := -1
		for _, o := range d.Options {
			if o.Kind == "pass" {
				pass = o.Index
			}
		}
		if pass < 0 {
			t.Fatalf("priority with no pass option: %+v", d.Options)
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{pass}}); err != nil {
			t.Fatalf("pass: %v", err)
		}
	}
	if !found {
		t.Fatalf("engine never handed seat 1 priority on the arm turn (active=%d turn=%d priority=%d)",
			e.G.Active, e.G.Turn, e.G.Priority)
	}
	// The pre-turn decision must reach seat 1 itself, never the controller:
	// CR 720.2 scopes the controller's authority to the controlled turn.
	if d.Player != 1 {
		t.Fatalf("seat 1's pre-turn priority (e.G.Priority=1) posed to %d, want seat 1 itself (control must not spill before the controlled turn)", d.Player)
	}
	replayCheck(t, e, cfg)
}
