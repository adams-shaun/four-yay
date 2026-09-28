package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// Regressions for the cardfuzz batch12 livelock (all real corpus cards).

// TestReezugGraveyardCastDoesNotFollowTheCardBack (cardfuzz batch12 line 1):
// Reezug, the Bonecobbler's "{T}: ... You may cast that card this turn"
// remembers the targeted graveyard card in an Effect whose may-play grant
// carries no ForgetOnMoved$. The remembered ObjID is stable across zones, so
// once Blood Pet was cast from the graveyard, resolved and sacrificed for
// {B}, the grant matched it again: cast for {B}, sacrifice for {B}, forever
// within the turn. CR 400.7: the card that returns to the graveyard is a new
// object the permission never named, so it is no longer castable.
func TestReezugGraveyardCastDoesNotFollowTheCardBack(t *testing.T) {
	t.Parallel()
	e, cfg := b5Engine(t, "Reezug, the Bonecobbler", "Blood Pet")
	reezug := searchMoveByName(t, e, "Reezug, the Bonecobbler", state.ZBattlefield)
	pet := searchMoveByName(t, e, "Blood Pet", state.ZGraveyard)
	// A logged TurnChange clears summoning sickness (CR 302.6) the
	// replayable way, then the clock is parked in main phase 1.
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 2})
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepMain1})
	addMana(t, e, 0, "BB")
	submitChoices(t, e, abilityOption(t, e, reezug, 0).Index)
	driveStackEmpty(t, e, 20, func(*decision.Decision) int { return 0 })
	if !castOffered(e, pet) {
		t.Fatal("Blood Pet is not castable from the graveyard after Reezug's ability")
	}
	submitChoices(t, e, castOptionFor(t, e, pet).Index)
	driveStackEmpty(t, e, 20, func(*decision.Decision) int { return 0 })
	if z := e.G.Obj(pet).Zone; z != state.ZBattlefield {
		t.Fatalf("Blood Pet rests in %v after its cast, want battlefield", z)
	}
	sac := -1
	for _, o := range e.Pending().Options {
		if o.Kind == "activate" && o.Obj == pet {
			sac = o.Index
		}
	}
	if sac < 0 {
		t.Fatalf("no mana-ability option for Blood Pet: %+v", e.Pending().Options)
	}
	submitChoices(t, e, sac)
	driveStackEmpty(t, e, 20, func(*decision.Decision) int { return 0 })
	if z := e.G.Obj(pet).Zone; z != state.ZGraveyard {
		t.Fatalf("Blood Pet rests in %v after its sacrifice, want graveyard", z)
	}
	if castOffered(e, pet) {
		t.Fatal("Blood Pet is castable from the graveyard again: Reezug's one-shot permission followed it back (CR 400.7)")
	}
	replayCheck(t, e, cfg)
}
