package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func armEffectLifetime(t *testing.T, dur, rider, trigger string, remembered ...state.ObjID) (*Engine, state.ObjID) {
	t.Helper()
	e := layerEngine(t)
	src := onBoard(t, e, 0, "Name:LifetimeSource\nTypes:Creature\nPT:2/2\nA:AB$ Effect | Triggers$ Hook | Duration$ "+dur+" | RememberObjects$ Remembered"+rider+"\nSVar:Hook:"+trigger+"\nSVar:Pain:DB$ LoseLife | Defined$ You | LifeAmount$ 2\nOracle:x\n")
	if e.G.Obj(src).Zone != state.ZBattlefield {
		t.Fatal("precondition: source not on battlefield")
	}
	face := e.G.Obj(src).Face()
	ctx := &effects.Ctx{Source: src, Controller: 0, SVars: face.SVars}
	for _, id := range remembered {
		ctx.Remembered = append(ctx.Remembered, state.Target{Obj: id})
	}
	effects.Resolve(e, ctx, face.Abilities[0])
	if len(e.G.Delayed) != 1 || len(effectNotesContaining(e, "unmodelled Effect trigger lifetime")) != 0 {
		t.Fatalf("precondition: Effect not armed: %+v notes=%v", e.G.Delayed, effectNoteTexts(e))
	}
	return e, src
}

func TestEffectDelayedPermanentAndNextTurn(t *testing.T) {
	t.Parallel()
	trigger := "Mode$ DamageDone | ValidTarget$ Player | Execute$ Pain"
	t.Run("permanent source leaves", func(t *testing.T) {
		e, src := armEffectLifetime(t, "Permanent", "", trigger)
		if e.G.Delayed[0].EffectDuration != "permanent" {
			t.Fatalf("wrong duration: %+v", e.G.Delayed)
		}
		e.emit(events.Event{Kind: events.Damage, Player: 1, Amount: 1})
		if len(e.pendingTriggers) != 1 {
			t.Fatalf("live source did not trigger: %+v", e.pendingTriggers)
		}
		e.pendingTriggers = nil
		e.emit(events.Event{Kind: events.MoveZone, Obj: src, From: state.ZBattlefield, To: state.ZGraveyard})
		if e.G.Obj(src).Zone == state.ZBattlefield {
			t.Fatal("precondition: source did not leave")
		}
		e.emit(events.Event{Kind: events.Damage, Player: 1, Amount: 1})
		if len(e.pendingTriggers) != 0 || len(e.G.Delayed) != 0 {
			t.Fatalf("dead source still fires: %+v %+v", e.pendingTriggers, e.G.Delayed)
		}
	})
	t.Run("next turn start", func(t *testing.T) {
		e, _ := armEffectLifetime(t, "UntilYourNextTurn", "", trigger)
		if e.G.Delayed[0].EffectDuration != "untilyournextturn" {
			t.Fatalf("wrong duration: %+v", e.G.Delayed)
		}
		e.emit(events.Event{Kind: events.Damage, Player: 1, Amount: 1})
		if len(e.pendingTriggers) != 1 {
			t.Fatal("precondition: promise did not fire before boundary")
		}
		e.pendingTriggers = nil
		old := e.G.Turn
		e.emit(events.Event{Kind: events.TurnChange, Player: 1, Amount: old + 1})
		if len(e.G.Delayed) != 1 {
			t.Fatal("expired on opponent turn")
		}
		e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: old + 2})
		if e.G.Turn <= old || len(e.G.Delayed) != 0 {
			t.Fatalf("controller's next turn did not retire promise: %+v", e.G.Delayed)
		}
		e.emit(events.Event{Kind: events.Damage, Player: 1, Amount: 1})
		if len(e.pendingTriggers) != 0 {
			t.Fatal("trigger after next turn")
		}
	})
	t.Run("next turn end", func(t *testing.T) {
		e, _ := armEffectLifetime(t, "UntilTheEndOfYourNextTurn", "", trigger)
		old := e.G.Turn
		e.emit(events.Event{Kind: events.TurnChange, Player: 1, Amount: old + 1})
		e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: old + 2})
		if len(e.G.Delayed) != 1 {
			t.Fatal("expired at START rather than end of next turn")
		}
		e.emit(events.Event{Kind: events.Damage, Player: 1, Amount: 1})
		if len(e.pendingTriggers) != 1 {
			t.Fatal("did not fire on next turn")
		}
		e.pendingTriggers = nil
		e.emit(events.Event{Kind: events.StepChange, Step: state.StepCleanup})
		if len(e.G.Delayed) != 0 {
			t.Fatal("did not expire at cleanup")
		}
	})
}

func TestEffectDelayedMoveEndAndForget(t *testing.T) {
	t.Parallel()
	for _, rider := range []string{"ExileOnMoved", "ForgetOnMoved"} {
		t.Run(rider, func(t *testing.T) {
			// Assemble one board so the remembered objects and the source belong
			// to this SAME game (and the positive control differs from the mover).
			e := layerEngine(t)
			src := onBoard(t, e, 0, "Name:MovePromise\nTypes:Creature\nPT:2/2\nA:AB$ Effect | Triggers$ Hook | RememberObjects$ Remembered | "+rider+"$ Battlefield\nSVar:Hook:Mode$ DamageDone | ValidTarget$ Creature.IsRemembered | Execute$ Pain\nSVar:Pain:DB$ LoseLife | Defined$ You | LifeAmount$ 2\nOracle:x\n")
			moved := onBoard(t, e, 0, "Name:Moved\nTypes:Creature\nPT:1/1\nOracle:x\n")
			kept := onBoard(t, e, 0, "Name:Kept\nTypes:Creature\nPT:1/1\nOracle:x\n")
			if moved == kept || e.G.Obj(moved).Zone != state.ZBattlefield || e.G.Obj(kept).Zone != state.ZBattlefield {
				t.Fatal("precondition: two distinct battlefield subjects required")
			}
			face := e.G.Obj(src).Face()
			effects.Resolve(e, &effects.Ctx{Source: src, Controller: 0, SVars: face.SVars, Remembered: []state.Target{{Obj: moved}, {Obj: kept}}}, face.Abilities[0])
			if len(e.G.Delayed) != 1 || len(e.G.Delayed[0].Remembered) != 2 {
				t.Fatalf("precondition: forgotten set not captured: %+v", e.G.Delayed)
			}
			e.emit(events.Event{Kind: events.Damage, Obj: moved, Amount: 1})
			if len(e.pendingTriggers) != 1 {
				t.Fatal("precondition: remembered mover did not match before moving")
			}
			e.pendingTriggers = nil
			e.emit(events.Event{Kind: events.MoveZone, Obj: moved, From: state.ZBattlefield, To: state.ZGraveyard})
			if e.G.Obj(moved).Zone != state.ZGraveyard {
				t.Fatal("precondition: card did not move")
			}
			e.emit(events.Event{Kind: events.Damage, Obj: moved, Amount: 1})
			if len(e.pendingTriggers) != 0 {
				t.Fatal("moved card matched after lifetime change")
			}
			if rider == "ExileOnMoved" {
				if len(e.G.Delayed) != 0 {
					t.Fatalf("exile did not end Effect: %+v", e.G.Delayed)
				}
			} else {
				if len(e.G.Delayed) != 1 || len(e.G.Delayed[0].Remembered) != 1 || e.G.Delayed[0].Remembered[0].Obj != kept {
					t.Fatalf("forget did not trim remembered set: %+v", e.G.Delayed)
				}
				e.emit(events.Event{Kind: events.Damage, Obj: kept, Amount: 1})
				if len(e.pendingTriggers) != 1 {
					t.Fatal("untouched remembered subject did not match")
				}
			}
		})
	}
}

// The real pinned script carries a Permanent phase trigger. The registration
// is tested here separately from the script's unrelated replacement grant.
func TestEffectDelayedBlindingBeamCorpus(t *testing.T) {
	c := choiceCorpusCard(t, "Blinding Beam")
	if c == nil {
		t.Fatal("precondition: missing real corpus card")
	}
	e := layerEngine(t)
	src := onBoardCard(t, e, 0, c)
	face := e.G.Obj(src).Face()
	sa := cards.ResolveSVar(face.SVars, "DBEffect")
	if sa == nil || e.G.Obj(src).Zone != state.ZBattlefield {
		t.Fatal("precondition: missing DBEffect or source battlefield")
	}
	// This is a registration test, not the spell's target-election test:
	// provide the already-selected player and skip the outer ValidTgts ask.
	body := *sa
	body.Params = make(map[string]string, len(sa.Params))
	for k, v := range sa.Params {
		if k != "ValidTgts" {
			body.Params[k] = v
		}
	}
	effects.Resolve(e, &effects.Ctx{Source: src, Controller: 0, SVars: face.SVars, Targets: []state.Target{{IsPlayer: true, Player: 1}}, Remembered: []state.Target{{IsPlayer: true, Player: 1}}}, &body)
	if len(e.G.Delayed) != 1 || e.G.Delayed[0].EffectDuration != "permanent" || e.G.Delayed[0].Phase != state.StepUntap {
		t.Fatalf("Blinding Beam did not arm its real permanent untap promise: %+v notes=%v", e.G.Delayed, effectNoteTexts(e))
	}
	e.emit(events.Event{Kind: events.TurnChange, Player: 1, Amount: e.G.Turn + 1})
	if len(e.G.Delayed) != 1 {
		t.Fatal("real card's permanent trigger expired on turn change")
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: src, From: state.ZBattlefield, To: state.ZGraveyard})
	if e.G.Obj(src).Zone == state.ZBattlefield {
		t.Fatal("precondition: real source did not leave battlefield")
	}
	if e.delayedRegistrationLive(&e.G.Delayed[0]) {
		t.Fatal("real permanent trigger survived its source leaving")
	}
}
