package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// Timothar, Baron of Bats end to end under the kernel: the dead Vampire is
// exiled by the paid trigger cost, the Bat remembers it, and the Bat's
// granted combat-damage trigger returns it tapped under seat 0's control.

// kr9WaitForWindow is waitForWindow resolving the stack as kernel probes.
func kr9WaitForWindow(t *testing.T, e *Engine) *decision.Decision {
	t.Helper()
	for i := 0; i < 4; i++ {
		d := e.Pending()
		if d != nil && d.Kind == decision.KChoose && len(d.Options) > 0 &&
			(d.Options[0].Kind == "trigger_cost_pay" || d.Options[0].Kind == "trigger_cost_decline") {
			return d
		}
		if d != nil && d.Kind != decision.KPriority {
			t.Fatalf("unexpected decision waiting for the window: %+v", d)
		}
		if len(e.G.Stack) == 0 {
			t.Fatal("stack empty but the triggered-cost window never opened")
		}
		kr9ResolveTop(e)
	}
	t.Fatal("the triggered-cost window never opened")
	return nil
}

func TestTokenRememberedTimotharEndToEndKernel(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg, ids := tokenRememberedBoard(t, reg, "Timothar, Baron of Bats", "Vampire Nighthawk")
	timothar := ids["Timothar, Baron of Bats"]
	vampire := ids["Vampire Nighthawk"]
	if timothar == 0 || vampire == 0 {
		t.Fatalf("board missing cards: timothar=%d vampire=%d", timothar, vampire)
	}
	if e.G.Obj(timothar).Zone != state.ZBattlefield || e.G.Obj(vampire).Zone != state.ZBattlefield {
		t.Fatalf("precondition: Timothar/Vampire must both start on the battlefield (%v/%v)",
			e.G.Obj(timothar).Zone, e.G.Obj(vampire).Zone)
	}
	if !strings.Contains(strings.ToLower(e.G.Obj(vampire).Face().Name), "vampire") {
		t.Fatalf("precondition: bearer %q is not a Vampire", e.G.Obj(vampire).Face().Name)
	}

	// {1} in the pool so the trigger's `Cost$ 1` is payable when the window
	// opens. The mana is added BEFORE the kill so the window's pay gate sees
	// it.
	addMana(t, e, 0, "C")

	// The other nontoken Vampire dies. The changes-zone trigger
	// (Origin$ Battlefield | Destination$ Graveyard | ValidCard$
	// Vampire.Other+!token+YouCtrl) fires Timothar's TrigToken body.
	e.emit(events.Event{Kind: events.MoveZone, Obj: vampire, From: state.ZBattlefield,
		To: state.ZGraveyard, Text: "destroyed"})
	e.putTriggersOnStack()

	// Resolve into the trigger situation; the cost window (the `Cost$ 1
	// ExileAnyGrave<1/Card.TriggeredNewCard>` half) opens as a pay/decline ask.
	d := kr9WaitForWindow(t, e)
	if !strings.Contains(d.Options[0].Label, "1") {
		t.Fatalf("Timothar's window pay label lost the {{1}}: %q", d.Options[0].Label)
	}
	submitChoices(t, e, d.Options[0].Index)

	// Precondition for the whole feature: paying exiled the triggering
	// Vampire (never left in the graveyard).
	if z := e.G.Obj(vampire).Zone; z != state.ZExile {
		t.Fatalf("paying the cost did not exile the dead Vampire: zone %v, log %+v", z, e.L.Events)
	}

	kr9Settle(e)
	// Let the Token + Animate chain resolve.
	passUntilStackEmpty(t, e, 30)

	// The Bat exists and REMEMBERS the exiled Vampire (TokenRemembered$
	// ExiledCards). This is the assertion the feature exists for.
	var bat state.ObjID
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		if o := e.G.Obj(id); o != nil && o.IsToken && o.Face() != nil &&
			strings.Contains(o.Face().Name, "Bat") {
			bat = id
		}
	}
	if bat == 0 {
		t.Fatalf("Timothar's token was not created; battlefield %v log %+v",
			e.G.Zone(state.ZBattlefield, 0), e.L.Events)
	}
	if !tokenRememberedCards(e, bat, vampire) {
		t.Fatalf("Bat token %d Remembered = %+v, want the exiled Vampire %d",
			bat, e.G.Obj(bat).Remembered, vampire)
	}
	// The Animate's Triggers$ CDTrigger must have granted the return trigger
	// to the Bat, or the damage assertions below are vacuous.
	if grants := triggerGrantsOn(e, bat); len(grants) != 1 {
		t.Fatalf("the Bat carries %d trigger grants, want 1 (the Animate's Triggers$ CDTrigger); log %+v",
			len(grants), e.L.Events)
	}

	// Drive to seat 0's NEXT turn so the Bat is free of summoning sickness and
	// can attack. The grant is Duration$ Permanent, so the granted trigger
	// survives the turn boundary. Every intervening declare-attackers step is
	// answered with no attackers.
	driveToAttackersAt(t, e, 4, 0)
	submitAttackers(t, e, bat)
	drainCombatDamagePriority(t, e)
	passUntilStackEmpty(t, e, 60)

	// The Bat dealt 1 combat damage to seat 1 (the granted trigger's gate).
	if got := e.G.Players[1].Life; got != 19 {
		t.Fatalf("seat 1 life = %d, want 19 (the Bat's 1 combat damage)", got)
	}
	// The trigger sacrificed the Bat and returned the exiled Vampire to the
	// battlefield TAPPED under seat 0's control.
	if o := e.G.Obj(bat); o == nil || o.Zone == state.ZBattlefield {
		t.Fatalf("the Bat was not sacrificed off the battlefield: %+v", o)
	}
	back := e.G.Obj(vampire)
	if back.Zone != state.ZBattlefield {
		t.Fatalf("the exiled Vampire was not returned: zone %v log %+v", back.Zone, e.L.Events)
	}
	if back.Controller != 0 {
		t.Fatalf("returned Vampire controller = %d, want seat 0", back.Controller)
	}
	if !back.Tapped {
		t.Fatal("returned Vampire did not enter tapped")
	}
	replayCheck(t, e, cfg)
}
