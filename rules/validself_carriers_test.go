package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func TestCheckOnTriggeredCardTotalDamageReceivedThisTurn(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	for _, tc := range []struct {
		name   string
		card   string
		amount int32
		want   int
	}{
		{name: "burning-eye-zubera", card: "Burning-Eye Zubera", amount: 4, want: 1},
		{name: "rushing-tide-zubera", card: "Rushing-Tide Zubera", amount: 4, want: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := crAbortEngine(t, reg, "ur-delver", tc.card)
			id := crAbortMove(t, e, 0, tc.card, state.ZBattlefield)
			o := e.G.Obj(id)
			if o == nil || o.Zone != state.ZBattlefield {
				t.Fatal("precondition: Zubera must be on the battlefield")
			}
			if got, ok := effects.EvalCountOK(e, &effects.Ctx{Source: id, Controller: 0, TriggerContext: effects.TriggerContext{TriggerCard: id}}, o.Face().SVars["X"]); !ok || got != 0 {
				t.Fatalf("precondition: damage count before hit = %d, resolved=%v; want 0,true", got, ok)
			}
			e.emit(events.Event{Kind: events.Damage, Obj: id, Amount: tc.amount})
			e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZBattlefield, To: state.ZGraveyard})
			e.pending = nil
			e.priorityRound()
			if got := crTriggerStackCount(e, id); got != tc.want {
				t.Fatalf("trigger stack count = %d, want %d after %d damage", got, tc.want, tc.amount)
			}
		})
	}
}

func TestCheckOnTriggeredCardEnteredLandCountUsesEventController(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := crAbortEngine(t, reg, "ur-delver", "Tunnel Ignus", "Plains")
	ignus := crAbortMove(t, e, 0, "Tunnel Ignus", state.ZBattlefield)
	first := crAbortMove(t, e, 1, "Plains", state.ZBattlefield)
	second := crAbortMove(t, e, 1, "Plains", state.ZBattlefield)
	if e.G.Obj(ignus).Zone != state.ZBattlefield || e.G.Obj(first).Controller != 1 || e.G.Obj(second).Controller != 1 {
		t.Fatal("precondition: Tunnel Ignus and both opposing lands must be on the battlefield under their intended controllers")
	}
	varX := e.G.Obj(ignus).Face().SVars["X"]
	if varX != "Count$ThisTurnEntered_Battlefield_Land.ControlledBy CardController" {
		t.Fatalf("fixture precondition: Tunnel Ignus X = %q", varX)
	}
	got, ok := effects.EvalCountOK(e, &effects.Ctx{Source: ignus, Controller: 0, TriggerContext: effects.TriggerContext{TriggerCard: second, TriggerCardController: state.Target{Player: 1, IsPlayer: true}}}, varX)
	if !ok || got != 2 {
		t.Fatalf("Tunnel Ignus count = %d, resolved=%v; want both lands controlled by the triggering player's controller", got, ok)
	}
}
