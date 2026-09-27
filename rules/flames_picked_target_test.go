package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func flamesPreaskedEffect(t *testing.T, e *Engine, parent, picked state.Target) {
	t.Helper()
	cardDef := corpusCard(t, "Flames of the Blood Hand")
	if cardDef.Faces[0].SVars["PreventHealing"] != "DB$ Effect | ReplacementEffects$ GainLifeEvent | RememberObjects$ TargetedOrController" {
		t.Fatalf("precondition: corpus PreventHealing SVar changed: %q", cardDef.Faces[0].SVars["PreventHealing"])
	}
	body := cards.ResolveSVar(cardDef.Faces[0].SVars, "PreventHealing")
	if body == nil || body.API != "Effect" {
		t.Fatalf("precondition: PreventHealing is not a compiled Effect: %+v", body)
	}
	// The corpus body has no ValidTgts$: add it only to this test copy to model
	// a pre-asked Effect target, without changing or pretending to change Forge.
	copySA := *body
	copySA.Params = make(map[string]string, len(body.Params)+1)
	for k, v := range body.Params {
		copySA.Params[k] = v
	}
	copySA.Params["ValidTgts"] = "Player,Planeswalker"
	pickedGroup := []state.Target{picked}
	if len(pickedGroup) != 1 {
		t.Fatal("precondition: test must provide exactly one picked target")
	}
	if parent == picked {
		t.Fatal("precondition: parent and sub-ability targets must differ")
	}
	source := onBoardCard(t, e, 0, cardDef)
	ctx := &effects.Ctx{Source: source, Controller: 0, SVars: cardDef.Faces[0].SVars,
		Targets: []state.Target{parent}, SubPreAsk: map[string][]state.Target{copySA.Line: pickedGroup}}
	before := len(e.continuous)
	effects.Resolve(e, ctx, &copySA)
	if ctx.PickedTargets != nil {
		t.Fatalf("precondition: temporary picked-target dispatch leaked: %v", ctx.PickedTargets)
	}
	if len(e.continuous) <= before {
		t.Fatal("precondition: real corpus Effect body did not register its GainLife replacement")
	}
}

func assertFlamesPreventsOnlyPlayer(t *testing.T, e *Engine, remembered state.PlayerID) {
	t.Helper()
	var found bool
	for _, ce := range e.continuous {
		if ce.ReplacementEvent == "GainLife" && len(ce.RememberedPlayers) == 1 && ce.RememberedPlayers[0] == remembered {
			found = true
		}
	}
	if !found {
		t.Fatalf("precondition/assertion: no GainLife registration captured player %d", remembered)
	}
	other := state.PlayerID(1 - remembered)
	lifeRemembered, lifeOther := e.G.Players[remembered].Life, e.G.Players[other].Life
	e.emit(events.Event{Kind: events.LifeChange, Player: remembered, Amount: 3})
	if e.G.Players[remembered].Life != lifeRemembered {
		t.Fatalf("remembered player %d gained life: got %d, want %d", remembered, e.G.Players[remembered].Life, lifeRemembered)
	}
	e.emit(events.Event{Kind: events.LifeChange, Player: other, Amount: 3})
	if e.G.Players[other].Life != lifeOther+3 {
		t.Fatalf("other player %d life = %d, want %d", other, e.G.Players[other].Life, lifeOther+3)
	}
}

func TestFlamesOfTheBloodHandPickedTargetPlayerRemembered(t *testing.T) {
	e := layerEngine(t)
	// The enclosing spell's player target is seat 0; this test-only Effect ask
	// independently answers seat 1.
	parent := state.Target{Player: 0, IsPlayer: true}
	picked := state.Target{Player: 1, IsPlayer: true}
	flamesPreaskedEffect(t, e, parent, picked)
	assertFlamesPreventsOnlyPlayer(t, e, 1)
}

func TestFlamesOfTheBloodHandPickedPlaneswalkerControllerRemembered(t *testing.T) {
	e := layerEngine(t)
	parent := state.Target{Player: 0, IsPlayer: true}
	walker := onBoard(t, e, 1, "Name:Test Walker\nTypes:Planeswalker\nLoyalty:3\nOracle:x\n")
	o := e.G.Obj(walker)
	if o == nil || o.Zone != state.ZBattlefield || o.Controller != 1 {
		t.Fatalf("precondition: picked planeswalker must be on battlefield under seat 1: %+v", o)
	}
	picked := state.Target{Obj: walker}
	if parent == picked || o.Controller == parent.Player {
		t.Fatal("precondition: planeswalker controller and parent target must differ")
	}
	flamesPreaskedEffect(t, e, parent, picked)
	assertFlamesPreventsOnlyPlayer(t, e, o.Controller)
}
