package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func TestTriggerRememberedFilterReachability(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	src := onBoard(t, e, 0, "Name:Watcher\nTypes:Creature\nPT:2/2\nOracle:x\n")
	captured := onBoard(t, e, 1, "Name:Captured\nTypes:Creature\nPT:2/2\nOracle:x\n")
	other := onBoard(t, e, 0, "Name:Other\nTypes:Creature\nPT:2/2\nOracle:x\n")
	if captured == other || e.G.Obj(captured).Zone != state.ZBattlefield || e.G.Obj(other).Zone != state.ZBattlefield || e.G.Obj(captured).Controller == e.G.Obj(other).Controller {
		t.Fatal("precondition: distinct battlefield creatures with different controllers required")
	}
	capture := []state.Target{{Obj: captured}}
	sc := effects.SpecContext{Source: src, You: 0, TriggerContext: effects.TriggerContext{DelayedRemembered: capture}}
	pc := effects.PlayerSpecCtx{Source: src, DelayedRemembered: capture}
	for _, tc := range []struct {
		name string
		sc   effects.SpecContext
		pc   effects.PlayerSpecCtx
		want bool
	}{
		{"bound", sc, pc, true},
		{"empty", effects.SpecContext{Source: src, You: 0}, effects.PlayerSpecCtx{Source: src}, false},
		{"absent", effects.SpecContext{}, effects.PlayerSpecCtx{}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := effects.MatchesSpecCtx(e.G, "Card.IsTriggerRemembered", captured, tc.sc); got != tc.want {
				t.Errorf("captured card = %v, want %v", got, tc.want)
			}
			if effects.MatchesSpecCtx(e.G, "Card.IsTriggerRemembered", other, tc.sc) {
				t.Error("unrelated card matched")
			}
			if tc.name != "bound" && effects.MatchesSpecCtx(e.G, "Card.!IsTriggerRemembered", other, tc.sc) {
				t.Error("negated predicate matched without a registration capture")
			}
			if got := effects.MatchesPlayerSpecCtx(e.G, "Player.controlsCard.IsTriggerRemembered", 1, 0, tc.pc); got != tc.want {
				t.Errorf("captured controller = %v, want %v", got, tc.want)
			}
			if effects.MatchesPlayerSpecCtx(e.G, "Player.controlsCard.IsTriggerRemembered", 0, 0, tc.pc) {
				t.Error("unrelated controller matched")
			}
		})
	}
}

// Inline delayed dies promise: the captured creature's death alone executes
// the body. The life delta is the observable stand-in for a token-creating
// Blessed Defiance body, without importing Forge's GPL script or token corpus.
func TestTriggerRememberedFilterDelayedAttackers(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	source := onBoard(t, e, 0, "Name:Watcher\nTypes:Creature\nPT:2/2\nOracle:x\n")
	captured := onBoard(t, e, 1, "Name:Captured\nTypes:Creature\nPT:2/2\nOracle:x\n")
	other := onBoard(t, e, 1, "Name:Other\nTypes:Creature\nPT:2/2\nOracle:x\n")
	if captured == other || e.G.Obj(captured).Zone != state.ZBattlefield || e.G.Obj(other).Zone != state.ZBattlefield {
		t.Fatal("precondition: distinct battlefield attackers required")
	}
	trig := cards.Trigger{Mode: "AttackersDeclared", Params: map[string]string{"ValidAttackers": "Card.IsTriggerRemembered"}}
	dt := &state.DelayedTrigger{Source: source, Remembered: []state.Target{{Obj: captured}}}
	if !e.delayedEventMatches(trig, dt, events.Event{Kind: events.DeclareAttackers, IDs: []state.ObjID{captured}}, nil) {
		t.Fatal("captured attacker did not match")
	}
	if e.delayedEventMatches(trig, dt, events.Event{Kind: events.DeclareAttackers, IDs: []state.ObjID{other}}, nil) {
		t.Fatal("unrelated attacker matched")
	}
}

func TestTriggerRememberedFilterDelayedDies(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	src := onBoard(t, e, 0, "Name:Defiance promise\nTypes:Creature Wizard\nPT:2/2\nSVar:Trig:DB$ GainLife | Defined$ You | LifeAmount$ 3\nOracle:x\n")
	captured := onBoard(t, e, 0, "Name:Protected\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	other := onBoard(t, e, 1, "Name:Unrelated\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	if captured == other || e.G.Obj(captured).Zone != state.ZBattlefield || e.G.Obj(other).Zone != state.ZBattlefield || e.G.Obj(src).Zone != state.ZBattlefield {
		t.Fatal("precondition: distinct battlefield creatures and source required")
	}
	e.emit(events.Event{Kind: events.DelayedRegister, Obj: src, Player: 0, Step: e.G.Step,
		Counter: "Trig", IDs: []state.ObjID{captured},
		Text: "ChangesZone:Mode$ ChangesZone | Origin$ Battlefield | Destination$ Graveyard | ValidCard$ Card.IsTriggerRemembered | Execute$ Trig"})
	if len(e.G.Delayed) != 1 || len(e.G.Delayed[0].Remembered) != 1 || e.G.Delayed[0].Remembered[0].Obj != captured {
		t.Fatalf("precondition: capture = %+v", e.G.Delayed)
	}
	life := e.G.Players[0].Life
	e.emit(events.Event{Kind: events.MoveZone, Obj: other, From: state.ZBattlefield, To: state.ZGraveyard})
	if len(e.pendingTriggers) != 0 || e.G.Players[0].Life != life || len(e.G.Delayed) != 1 {
		t.Fatalf("unrelated death fired: pending=%d life=%d delayed=%d", len(e.pendingTriggers), e.G.Players[0].Life, len(e.G.Delayed))
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: captured, From: state.ZBattlefield, To: state.ZGraveyard})
	if len(e.pendingTriggers) != 1 {
		t.Fatalf("captured death pending=%d, want 1", len(e.pendingTriggers))
	}
	e.putTriggersOnStack()
	if len(e.G.Stack) == 0 {
		t.Fatal("no delayed execute on stack")
	}
	e.resolveTop()
	if got := e.G.Players[0].Life; got != life+3 {
		t.Errorf("execute life=%d, want %d", got, life+3)
	}
}

func TestTriggerRememberedFilterDamageSource(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	src := onBoard(t, e, 0, "Name:Damage watcher\nTypes:Creature\nPT:2/2\nSVar:Trig:DB$ GainLife | Defined$ You | LifeAmount$ 2\nOracle:x\n")
	captured := onBoard(t, e, 0, "Name:Captured\nTypes:Creature\nPT:2/2\nOracle:x\n")
	other := onBoard(t, e, 0, "Name:Other\nTypes:Creature\nPT:2/2\nOracle:x\n")
	if captured == other || e.G.Obj(captured).Zone != state.ZBattlefield || e.G.Obj(other).Zone != state.ZBattlefield {
		t.Fatal("precondition: distinct battlefield damage sources required")
	}
	e.emit(events.Event{Kind: events.DelayedRegister, Obj: src, Player: 0, Step: e.G.Step,
		Counter: "Trig", IDs: []state.ObjID{captured},
		Text: "DamageDone:Mode$ DamageDone | ValidSource$ Card.IsTriggerRemembered | ValidTarget$ Player | Execute$ Trig"})
	if len(e.G.Delayed) != 1 || len(e.G.Delayed[0].Remembered) != 1 || e.G.Delayed[0].Remembered[0].Obj != captured {
		t.Fatalf("precondition: damage source capture = %+v", e.G.Delayed)
	}
	e.SetDamageSource(other)
	e.emit(events.Event{Kind: events.Damage, Player: 1, Amount: 1})
	if len(e.pendingTriggers) != 0 || len(e.G.Delayed) != 1 {
		t.Fatalf("unrelated source fired: pending=%d delayed=%d", len(e.pendingTriggers), len(e.G.Delayed))
	}
	e.SetDamageSource(captured)
	e.emit(events.Event{Kind: events.Damage, Player: 1, Amount: 1})
	e.SetDamageSource(0)
	if len(e.pendingTriggers) != 1 {
		t.Fatalf("captured damage source pending=%d, want 1", len(e.pendingTriggers))
	}
}

func TestTriggerRememberedFilterPhasePlayer(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	src := onBoard(t, e, 0, "Name:Promise\nTypes:Creature\nPT:2/2\nSVar:Trig:DB$ GainLife | Defined$ You | LifeAmount$ 2\nOracle:x\n")
	captured := onBoard(t, e, 1, "Name:Captured\nTypes:Creature\nPT:2/2\nOracle:x\n")
	if e.G.Obj(captured).Zone != state.ZBattlefield || e.G.Obj(captured).Controller != 1 || e.G.Obj(src).Zone != state.ZBattlefield || e.G.Active == 1 {
		t.Fatal("precondition: captured card on seat 1 battlefield; active seat differs")
	}
	e.emit(events.Event{Kind: events.DelayedRegister, Obj: src, Player: 0, Step: state.StepEnd,
		Counter: "Trig", IDs: []state.ObjID{captured}, Text: "End of Turn|VP=Player.controlsCard.IsTriggerRemembered"})
	if len(e.G.Delayed) != 1 || len(e.G.Delayed[0].Remembered) != 1 || e.G.Delayed[0].Remembered[0].Obj != captured || e.G.Delayed[0].ValidPlayer != "Player.controlsCard.IsTriggerRemembered" {
		t.Fatalf("precondition: capture / player gate = %+v", e.G.Delayed)
	}
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepEnd})
	if len(e.pendingTriggers) != 0 || len(e.G.Delayed) != 1 {
		t.Fatalf("wrong player's end step fired: pending=%d delayed=%d", len(e.pendingTriggers), len(e.G.Delayed))
	}
	e.emit(events.Event{Kind: events.TurnChange, Player: 1, Amount: e.G.Turn + 1})
	if e.G.Active != 1 || e.G.Obj(captured).Zone != state.ZBattlefield {
		t.Fatal("precondition: captured card must remain on active player's battlefield")
	}
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepEnd})
	if len(e.pendingTriggers) != 1 {
		t.Fatalf("captured controller's end step pending=%d, want 1", len(e.pendingTriggers))
	}
	life := e.G.Players[0].Life
	e.putTriggersOnStack()
	if len(e.G.Stack) == 0 {
		t.Fatal("no delayed player-gated execute on stack")
	}
	e.resolveTop()
	if got := e.G.Players[0].Life; got != life+2 {
		t.Errorf("execute life=%d, want %d", got, life+2)
	}
}
