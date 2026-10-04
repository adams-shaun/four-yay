package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestChangeZoneOriginAlternativeTowerWinderGraveyardKernel pins Tower
// Winder's `Origin$ Library | OriginAlternative$ Graveyard` ETB search: the
// graveyard Command Tower is offered (and is the only option: the hand copy
// sits outside the named origins), and the pick moves it to the searcher's
// hand.
func TestChangeZoneOriginAlternativeTowerWinderGraveyardKernel(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := searchEngine(t, reg, "Tower Winder", "Command Tower", "Command Tower")
	tower := searchMoveByName(t, e, "Command Tower", state.ZGraveyard)
	towerHand := searchMoveByName(t, e, "Command Tower", state.ZHand)
	wind := searchMoveByName(t, e, "Tower Winder", state.ZHand)
	if o := e.G.Obj(tower); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("precondition: Command Tower zone = %+v, want graveyard", o)
	}
	if o := e.G.Obj(towerHand); o == nil || o.Zone != state.ZHand {
		t.Fatalf("precondition: hand Command Tower zone = %+v, want hand", o)
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: wind, From: state.ZHand, To: state.ZBattlefield})
	e.putTriggersOnStack()
	e.pending = nil
	e.resolveTop()

	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "search" {
		t.Fatalf("Tower Winder ETB search pending = %+v, want a search KChoose", d)
	}
	if len(d.Options) != 1 || d.Options[0].Obj != tower {
		t.Fatalf("search options = %+v, want exactly the graveyard Command Tower %d", d.Options, tower)
	}
	if kr4Offers(d, towerHand) {
		t.Fatalf("hand Command Tower offered although hand is not a named origin: %+v", d.Options)
	}
	submitChoices(t, e, d.Options[0].Index)
	kr4Settle(e)
	passUntilStackEmpty(t, e, 30)
	if o := e.G.Obj(tower); o == nil || o.Zone != state.ZHand || o.Controller != 0 {
		t.Fatalf("Command Tower = %+v, want in the searcher's hand", o)
	}
	replayCheck(t, e, cfg)
}
