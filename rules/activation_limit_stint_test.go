package rules

// CR 400.7 / 602.5b: an activation limit belongs to the OBJECT. ObjID is
// stable for the match, but a permanent that leaves the battlefield and
// returns is a new object, so its "Activate only once" (GameActivationLimit$),
// "only once each turn" (ActivationLimit$), Exhaust and Power-up abilities are
// available again. The per-object counts are bounded at objectStintStart, a
// pure fold of the log, so a log-only replay derives the same answer.

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func TestActivationLimitsResetOnZoneChange(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, param string
	}{
		{"game-limit", "GameActivationLimit$ 1"},
		{"turn-limit", "ActivationLimit$ 1"},
		{"exhaust", "Exhaust$ True"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			src := "Name:StintBeast\nManaCost:0\nTypes:Creature Beast\nPT:2/2\n" +
				"A:AB$ Pump | Cost$ 1 G | Defined$ Self | Power$ 1 | Toughness$ 1 | " + tc.param +
				" | SpellDescription$ CARDNAME gets +1/+1. Activate only once.\nOracle:x\n"
			e, cfg, id := newFixtureDeck(t, 17, src)
			moveByName(t, e, 0, "StintBeast", state.ZBattlefield)
			addMana(t, e, 0, "GGGGGG")

			submitChoices(t, e, abilityOption(t, e, id, 0).Index)
			passUntilStackEmpty(t, e, 400)
			addMana(t, e, 0, "")
			if _, ok := findAbilityOption(e, id, 0); ok {
				t.Fatalf("limit not enforced on the same object: %+v", e.Pending().Options)
			}

			// Bounce and replay it the same turn: a new object (CR 400.7).
			e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZBattlefield, To: state.ZHand})
			e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZBattlefield})
			addMana(t, e, 0, "")
			if _, ok := findAbilityOption(e, id, 0); !ok {
				t.Fatalf("the returned permanent is a new object, its ability must be offered again: %+v", e.Pending().Options)
			}
			submitChoices(t, e, abilityOption(t, e, id, 0).Index)
			passUntilStackEmpty(t, e, 400)
			addMana(t, e, 0, "")
			if _, ok := findAbilityOption(e, id, 0); ok {
				t.Fatalf("the new object's own activation must spend its limit: %+v", e.Pending().Options)
			}
			if diff := diffGames(e.G, replayFromLog(t, cfg, e.L.Events)); diff != "" {
				t.Fatalf("log-only replay differs:\n%s", diff)
			}
		})
	}
}

// TestTriggerLimitsResetWhenTheSourceIsBlinked: "This ability triggers only
// once each turn" (ActivationLimit$) and "only once" (GameActivationLimit$)
// on a T: line belong to the object too. A source exiled and returned the
// same turn is a new object (CR 400.7) whose trigger is available again; the
// same object stays limited.
func TestTriggerLimitsResetWhenTheSourceIsBlinked(t *testing.T) {
	t.Parallel()
	for _, param := range []string{"ActivationLimit$ 1", "GameActivationLimit$ 1"} {
		t.Run(param, func(t *testing.T) {
			t.Parallel()
			e := layerEngine(t)
			id := onBoard(t, e, 0, "Name:Loser\nTypes:Creature\n"+
				"T:Mode$ LifeLost | ValidPlayer$ Opponent | TriggerZones$ Battlefield | "+param+" | Execute$ TrigDraw\n"+
				"SVar:TrigDraw:DB$ Draw\nOracle:x\n")
			e.emit(events.Event{Kind: events.LifeChange, Player: 1, Amount: -1})
			e.emit(events.Event{Kind: events.LifeChange, Player: 1, Amount: -1})
			if n := queuedFrom(e, id); n != 1 {
				t.Fatalf("same object, two losses: %d queued, want 1", n)
			}
			e.pendingTriggers = nil
			e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZBattlefield, To: state.ZExile})
			e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZExile, To: state.ZBattlefield})
			e.pendingTriggers = nil
			e.emit(events.Event{Kind: events.LifeChange, Player: 1, Amount: -1})
			if n := queuedFrom(e, id); n != 1 {
				t.Fatalf("the blinked permanent is a new object: %d queued, want 1", n)
			}
			e.emit(events.Event{Kind: events.LifeChange, Player: 1, Amount: -1})
			if n := queuedFrom(e, id); n != 1 {
				t.Fatalf("the new object's own trigger spends its limit: %d queued, want still 1", n)
			}
		})
	}
}
