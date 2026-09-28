package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func TestManaExpendPlayerSpecAndSVarAmount(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusAlternativeCard(t, "Teapot Slinger"))
	source := e.G.Zone(state.ZHand, 0)[0]
	if got := e.G.Obj(source); got == nil || got.Face() == nil || got.Face().Name != "Teapot Slinger" {
		t.Fatalf("test precondition: expected real Teapot Slinger source, got %+v", got)
	}
	placeOnBattlefield(t, e, source)
	if got := e.G.Obj(source); got == nil || got.Zone != state.ZBattlefield {
		t.Fatalf("test precondition: Teapot Slinger source is not on the battlefield: %+v", got)
	}
	trigger := cards.Trigger{Mode: "ManaExpend", Params: map[string]string{
		"Player": "Opponent", "Amount": "Count$YourLifeTotal",
	}}
	// This inline Count$ amount resolves from the game state (20 life), rather
	// than requiring Amount$ to be a numeric literal.
	e.manaExpendAdd(1, 20)
	ev := events.Event{Kind: events.CastInfo, Player: 1, Amount: 20, Counter: events.FlagsString(state.FlagManaExpendCast)}
	if e.controllerOf(source) != 0 || ev.Player == e.controllerOf(source) {
		t.Fatal("test precondition: Opponent selector must identify the other seat")
	}
	if got := e.manaExpendTotal(ev.Player); got != 20 || got != ev.Amount {
		t.Fatalf("test precondition: matcher threshold crossing not established: total=%d event amount=%d", got, ev.Amount)
	}
	if !e.manaExpendMatches(trigger, source, ev, nil) {
		t.Fatal("ManaExpend did not match Player$ Opponent with an SVar Amount$")
	}
}

func TestManaExpendPlayerSpecDoesNotMatchWrongSeat(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusAlternativeCard(t, "Teapot Slinger"))
	source := e.G.Zone(state.ZHand, 0)[0]
	placeOnBattlefield(t, e, source)
	if got := e.G.Obj(source); got == nil || got.Zone != state.ZBattlefield {
		t.Fatalf("test precondition: Teapot Slinger source is not on the battlefield: %+v", got)
	}
	trigger := cards.Trigger{Mode: "ManaExpend", Params: map[string]string{
		"Player": "Opponent", "Amount": "4",
	}}
	e.manaExpendAdd(0, 4)
	ev := events.Event{Kind: events.CastInfo, Player: 0, Amount: 4, Counter: events.FlagsString(state.FlagManaExpendCast)}
	if e.controllerOf(source) != 0 || ev.Player != e.controllerOf(source) {
		t.Fatal("test precondition: event seat should be source controller, not Player$ Opponent")
	}
	if e.manaExpendMatches(trigger, source, ev, nil) {
		t.Fatal("ManaExpend matched the source controller for Player$ Opponent")
	}
}

// TestManaExpendAbsentPlayerSpecIsControllerOnly pins the regression the
// selector grammar introduced: an OMITTED Player$ must keep the historical
// "whenever YOU expend" reading, so an opponent's payment must NOT match.
func TestManaExpendAbsentPlayerSpecIsControllerOnly(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusAlternativeCard(t, "Teapot Slinger"))
	source := e.G.Zone(state.ZHand, 0)[0]
	placeOnBattlefield(t, e, source)
	if got := e.G.Obj(source); got == nil || got.Zone != state.ZBattlefield {
		t.Fatalf("test precondition: Teapot Slinger source is not on the battlefield: %+v", got)
	}
	trigger := cards.Trigger{Mode: "ManaExpend", Params: map[string]string{"Amount": "4"}}
	if _, ok := trigger.Params["Player"]; ok {
		t.Fatal("test precondition: Player$ must be absent for this regression")
	}
	// The controller's own payment at the threshold DOES match: an absent
	// Player$ is not a blanket no-fire.
	e.manaExpendAdd(0, 4)
	evtCtrl := events.Event{Kind: events.CastInfo, Player: 0, Amount: 4, Counter: events.FlagsString(state.FlagManaExpendCast)}
	if e.controllerOf(source) != 0 || evtCtrl.Player != e.controllerOf(source) {
		t.Fatal("test precondition: control event must be the source controller")
	}
	if !e.manaExpendMatches(trigger, source, evtCtrl, nil) {
		t.Fatal("ManaExpend with absent Player$ did not match the controller's own payment")
	}
	// An opponent's payment MIGHT independently cross the tally, so set one
	// up that does and assert it is still rejected by the absent selector.
	e.manaExpendAdd(1, 4)
	evtOpp := events.Event{Kind: events.CastInfo, Player: 1, Amount: 4, Counter: events.FlagsString(state.FlagManaExpendCast)}
	if evtOpp.Player == e.controllerOf(source) {
		t.Fatal("test precondition: opponent event must not be the source controller")
	}
	if got := e.manaExpendTotal(evtOpp.Player); got != 4 || evtOpp.Amount != 4 {
		t.Fatalf("test precondition: opponent crossing not established: total=%d amount=%d", got, evtOpp.Amount)
	}
	if e.manaExpendMatches(trigger, source, evtOpp, nil) {
		t.Fatal("ManaExpend with absent Player$ matched an opponent's payment")
	}
}
