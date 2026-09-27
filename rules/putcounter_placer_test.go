package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/state"
)

func TestPutCounterPlacerDrivesPlayerScopedTrigger(t *testing.T) {
	e := layerEngine(t)
	watcher := func(name string) *cards.Card {
		return card(t, "Name:"+name+"\nTypes:Creature\nPT:1/1\n"+
			"T:Mode$ CounterPlayerAddedAll | ValidSource$ You | TriggerZones$ Battlefield | Execute$ Draw\n"+
			"SVar:Draw:DB$ Draw | Defined$ You | NumCards$ 1\nOracle:x\n")
	}
	w0 := onBoardCard(t, e, 0, watcher("Remembered Placer watcher"))
	w1 := onBoardCard(t, e, 1, watcher("Controller Placer watcher"))
	target := onBoardCard(t, e, 1, card(t, "Name:Counter target\nTypes:Creature\nPT:1/1\nOracle:x\n"))
	abilitySource := onBoardCard(t, e, 1, card(t, "Name:Put counter source\nTypes:Creature\nPT:1/1\nOracle:x\n"))
	if e.G.Obj(w0).Zone != state.ZBattlefield || e.G.Obj(w1).Zone != state.ZBattlefield ||
		e.G.Obj(target).Zone != state.ZBattlefield || e.G.Obj(target).Controller != 1 ||
		e.G.Obj(abilitySource).Zone != state.ZBattlefield {
		t.Fatal("precondition: both watchers, the target, and the resolving source must be on the battlefield")
	}
	for _, id := range []state.ObjID{w0, w1} {
		if len(e.G.Obj(id).Face().Triggers) != 1 {
			t.Fatalf("precondition: watcher %d has no single parsed trigger", id)
		}
	}

	// The effect controller is seat 1, but Player.IsRemembered identifies seat 0.
	// The two watchers' You-scoped trigger results must therefore be opposite.
	sa := card(t, "Name:Put effect\nTypes:Instant\n"+
		"A:DB$ PutCounter | Defined$ Targeted | CounterType$ P1P1 | CounterNum$ 1 | Placer$ Player.IsRemembered\nOracle:x\n").Faces[0].Abilities[0]
	effects.Resolve(e, &effects.Ctx{Source: abilitySource, Controller: 1,
		Targets: []state.Target{{Obj: target}}, Remembered: []state.Target{{Player: 0, IsPlayer: true}}}, sa)
	if got := e.G.Obj(target).Counter("P1P1"); got != 1 {
		t.Fatalf("precondition: PutCounter did not place a counter on the battlefield target: got %d", got)
	}
	fired := map[state.ObjID]bool{}
	for _, pt := range e.pendingTriggers {
		fired[pt.Source] = true
	}
	if !fired[w0] || fired[w1] {
		t.Fatalf("object CounterPlayerAddedAll You trigger sources = %v, want remembered placer watcher %d only (controller watcher %d must not fire)", fired, w0, w1)
	}

	// PlayerCounterChange is a separate ordinary PutCounter branch; it must
	// publish the same placer role rather than falling back to the controller.
	e.pendingTriggers = nil
	playerSA := card(t, "Name:Put player counter\nTypes:Instant\n"+
		"A:DB$ PutCounter | Defined$ Targeted | CounterType$ ENERGY | CounterNum$ 1 | Placer$ Player.IsRemembered\nOracle:x\n").Faces[0].Abilities[0]
	effects.Resolve(e, &effects.Ctx{Source: abilitySource, Controller: 1,
		Targets: []state.Target{{Player: 1, IsPlayer: true}}, Remembered: []state.Target{{Player: 0, IsPlayer: true}}}, playerSA)
	if got := e.G.Players[1].Counter("ENERGY"); got != 1 {
		t.Fatalf("precondition: player-target PutCounter did not add ENERGY to seat 1: got %d", got)
	}
	fired = map[state.ObjID]bool{}
	for _, pt := range e.pendingTriggers {
		fired[pt.Source] = true
	}
	if !fired[w0] || fired[w1] {
		t.Fatalf("player CounterPlayerAddedAll You trigger sources = %v, want remembered placer watcher %d only (controller watcher %d must not fire)", fired, w0, w1)
	}
}
