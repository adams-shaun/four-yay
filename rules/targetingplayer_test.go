package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// This file pins TargetingPlayer$ as a target-time CHOOSER redirect on the
// triggered-ability target ask (rules/stack.go askTarget), not a
// resolution-time attribution. Bladegriff Prototype is the corpus carrier:
// its combat-damage trigger's destroy body carries
// `TargetingPlayer$ TriggeredTarget`, so the damaged player answers the
// target ask while ValidTgts$ ...OppCtrl stays relative to the Griffin's
// controller. The unit arms pin the fail-closed contract for an unknown,
// unbound or dead referent (the ask stays with the ability's controller).

// TestBladegriffPrototypeChoosingPlayerIsTheDamagedSeat drives the real
// corpus script through combat: the Griffin attacks seat 1, its DamageDone
// trigger goes on the stack with TriggeredTarget bound to the damaged seat,
// and the pending KTarget ask must be posed to seat 1 -- with the only
// option being seat 1's nonland permanent -- because TargetingPlayer$
// TriggeredTarget names the chooser. Answering it destroys that permanent
// and the whole game replays.
func TestBladegriffPrototypeChoosingPlayerIsTheDamagedSeat(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := combatTriggerBoard(t, reg,
		[]string{"Bladegriff Prototype"},
		[]string{"Name:Decoy\nManaCost:0\nTypes:Artifact\nOracle:x\n"},
		nil,
		[]string{"Name:Victim\nManaCost:1 G\nTypes:Creature Bear\nPT:0/2\nOracle:x\n"})

	// Precondition: the corpus carrier really carries the parameter under
	// test -- a build that lost the line would otherwise pass this test
	// vacuously with the ask defaulted to the controller.
	griffinCard := mustCorpusCard(t, reg, "Bladegriff Prototype")
	if !hasTargetingPlayerTrigger(griffinCard, "TriggeredTarget") {
		t.Fatal("Bladegriff Prototype's compiled trigger no longer carries TargetingPlayer$ TriggeredTarget -- fixture premise broken")
	}

	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 2})
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepDeclareAttackers})
	griffin := findBattlefield(t, e, 0, "Bladegriff Prototype", 0)
	victim := findBattlefield(t, e, 1, "Victim", 0)
	if victim == 0 {
		t.Fatal("seat 1 has no Victim on the battlefield -- the target-hungry trigger would fizzle with no ask")
	}
	decoy := findBattlefield(t, e, 0, "Decoy", 0)
	if decoy == 0 {
		t.Fatal("seat 0 has no Decoy on the battlefield")
	}

	// Precondition: the Victim is a nonland permanent controlled by an
	// opponent of the ability's controller, i.e. a legal target for the
	// body's ValidTgts$ Permanent.nonLand+OppCtrl.
	vo := e.G.Obj(victim)
	if vo == nil || vo.Controller != 1 || vo.Face() == nil || vo.Face().IsLand() {
		t.Fatalf("Victim precondition failed: %+v", vo)
	}

	e.askAttackers()
	submitAttackers(t, e, griffin)
	drainCombatDamagePriority(t, e)

	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected the Griffin's trigger target ask, got %+v", d)
	}
	// The defect: the ask must go to the damaged player (seat 1), not the
	// trigger's controller (seat 0).
	if d.Player != 1 {
		t.Fatalf("target ask posed to seat %d, want the damaged player seat 1", d.Player)
	}
	// The candidate set must stay relative to the ability's controller: only
	// seat 1's Victim is offered, never seat 0's Decoy.
	if len(d.Options) != 1 || d.Options[0].Obj != victim {
		t.Fatalf("target options = %+v, want exactly seat 1's Victim %d", d.Options, victim)
	}

	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{d.Options[0].Index}}); err != nil {
		t.Fatalf("submit the damaged player's target choice: %v", err)
	}
	passUntilStackEmpty(t, e, 20)
	if z := e.G.Obj(victim).Zone; z != state.ZGraveyard {
		t.Fatalf("Victim zone = %s, want graveyard", z)
	}
	if z := e.G.Obj(decoy).Zone; z != state.ZBattlefield {
		t.Fatalf("seat 0's Decoy moved to %s -- the chooser must not be able to pick it", z)
	}
	replayCheck(t, e, cfg)
}

// hasTargetingPlayerTrigger reports whether card has any trigger whose
// execute body carries TargetingPlayer$ equal to spec. It is the fixture
// guard: the end-to-end test's premise is the compiled script.
func hasTargetingPlayerTrigger(card *cards.Card, spec string) bool {
	for _, f := range card.Faces {
		for _, t := range f.Triggers {
			if t.Effect != nil && t.Effect.Params["TargetingPlayer"] == spec {
				return true
			}
		}
	}
	return false
}

// TestTargetChooserInvalidPlayerIDFailsClosed pins the bounds check for an
// invalid player-valued trigger binding. PlayerID is uint8, so a negative
// value cannot be represented: casting -1 from an external integer produces
// 255, which must fail closed without indexing the players slice.
func TestTargetChooserInvalidPlayerIDFailsClosed(t *testing.T) {
	t.Parallel()
	e, _ := combatTriggerBoard(t, testutil.CorpusRegistry(t), nil, nil, nil, nil)
	const controller state.PlayerID = 0
	negative := -1
	invalid := state.PlayerID(negative) // conversion wraps to 255
	if negative >= 0 || invalid != 255 || len(e.G.Players) < 2 || int(invalid) < len(e.G.Players) || invalid == controller {
		t.Fatalf("invalid-player fixture broken: players=%d, invalid=%d, controller=%d", len(e.G.Players), invalid, controller)
	}
	tc := effects.TriggerContext{TriggerTarget: state.Target{IsPlayer: true, Player: invalid}}
	if who, ok := e.targetChooserFromSpec("TriggeredTarget", controller, nil, tc); ok || who != controller {
		t.Fatalf("invalid player referent = (%d, %v), want (%d, false)", who, ok, controller)
	}
}

// TestTargetChooserFromSpecFailsClosed pins the resolver's contract: a known
// trigger-relative referent resolves to its seat; an unknown spelling, an
// unbound role and a dead referent all fail closed so the ask stays with the
// ability's controller.
func TestTargetChooserFromSpecFailsClosed(t *testing.T) {
	t.Parallel()
	e, _ := combatTriggerBoard(t, testutil.CorpusRegistry(t), nil, nil, nil, nil)
	const controller state.PlayerID = 0

	damaged := state.Target{Player: 1, IsPlayer: true}
	tc := effects.TriggerContext{TriggerTarget: damaged}

	// Known referent: the damaged player.
	if who, ok := e.targetChooserFromSpec("TriggeredTarget", controller, nil, tc); !ok || who != 1 {
		t.Fatalf("TriggeredTarget = (%d, %v), want (1, true)", who, ok)
	}
	// Same role via the TriggerPlayer binding.
	tp := effects.TriggerContext{TriggerPlayer: damaged}
	if who, ok := e.targetChooserFromSpec("TriggeredPlayer", controller, nil, tp); !ok || who != 1 {
		t.Fatalf("TriggeredPlayer = (%d, %v), want (1, true)", who, ok)
	}
	// Opponent is resolved by targetChooserCore, which can pose the
	// controller's selection ask; the trigger-relative resolver must not
	// silently pick the first seat.
	if who, ok := e.targetChooserFromSpec("Opponent", controller, nil, tc); ok || who != controller {
		t.Fatalf("Opponent = (%d, %v), want fail-closed (%d, false)", who, ok, controller)
	}
	if who, ok := e.targetChooserFromSpec("Player.Opponent", controller, nil, tc); ok || who != controller {
		t.Fatalf("Player.Opponent = (%d, %v), want fail-closed (%d, false)", who, ok, controller)
	}

	// Unknown spelling fails closed to the controller.
	if who, ok := e.targetChooserFromSpec("NoSuchReferent", controller, nil, tc); ok || who != controller {
		t.Fatalf("unknown spec = (%d, %v), want (%d, false)", who, ok, controller)
	}
	// Unbound role (an object-only TriggerTarget, not a player) fails closed.
	objOnly := effects.TriggerContext{TriggerTarget: state.Target{Obj: 99}}
	if who, ok := e.targetChooserFromSpec("TriggeredTarget", controller, nil, objOnly); ok || who != controller {
		t.Fatalf("unbound role = (%d, %v), want (%d, false)", who, ok, controller)
	}
	// Dead referent fails closed.
	e.G.Players[1].Lost = true
	if who, ok := e.targetChooserFromSpec("TriggeredTarget", controller, nil, tc); ok || who != controller {
		t.Fatalf("dead referent = (%d, %v), want (%d, false)", who, ok, controller)
	}
}
