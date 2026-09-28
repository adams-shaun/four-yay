package rules

// Task fx27 — "drawCard runs state-based actions unconditionally, including
// mid-resolution".
//
// This file records the investigation of rules/engine.go drawCard and PINS
// the behaviour that makes it correct, so a future refactor that routes a
// mid-resolution draw through the state-based-action-bearing path fails
// loudly instead of silently moving when SBAs run.
//
// The engine intentionally has TWO draw paths:
//
//	e.drawCard -> effects.DrawFor + e.checkStateBased   (rules/engine.go:648)
//	              REACHED ONLY FROM a priority boundary:
//	                - the opening-hand deal       rules/engine.go:415
//	                - the London mulligan redraw  rules/mulligan.go:226
//	                - the turn draw step          rules/turn.go:361
//
//	effects.DrawFor via the Draw primitive              (effects/cardflow.go
//	effDraw, callable directly as rules/resolveAbility -> effects.Resolve)
//	              REACHED FROM the resolution of a card that says "draw a
//	              card" — and it does NOT call e.checkStateBased. That is
//	              the CR 704.4 guarantee: SBAs are checked only when a
//	              player would receive priority, never in the middle of a
//	              spell or ability resolving.
//
// So the unconditional e.checkStateBased() at engine.go:650 is NOT reachable
// from a resolution: the two paths share effects.DrawFor but not the SBA
// tail. The test below pins that CR 704.4 behaviour for the resolution draw:
// a lethal-damage creature must survive the draw and only die at the next
// priority boundary. It fails if the resolution draw path ever starts running
// state-based actions mid-resolution (family F20).

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func TestDrawDuringResolutionDefersStateBasedActions(t *testing.T) {
	t.Parallel()
	e := crResolutionEngine(t, []string{"Weave Fate"}, nil)

	// Seat 1 (death-n-taxes) runs a real 3/3 Serra Avenger. Mark 3 lethal
	// damage on it — an outstanding CR 704.5b condition.
	avenger := crAbortMove(t, e, 1, "Serra Avenger", state.ZBattlefield)
	e.emit(events.Event{Kind: events.Damage, Obj: avenger, Amount: 3})
	if e.G.Obj(avenger).Zone != state.ZBattlefield {
		t.Fatalf("fixture: Serra Avenger left %s after 3 damage marking", e.G.Obj(avenger).Zone)
	}

	// Resolve a REAL "draw two cards" spell (Weave Fate, A:SP$ Draw | NumCards$ 2).
	draw := crAbortMove(t, e, 0, "Weave Fate", state.ZHand)
	f := e.G.Obj(draw).Face()
	sa := f.SpellAbility()
	if sa == nil || sa.API != "Draw" {
		t.Fatalf("Weave Fate fixture changed: spell ability=%+v", sa)
	}
	drawsBefore := len(e.G.Zone(state.ZHand, 0))
	start := len(e.L.Events)
	e.resolveAbility(draw, 0, nil, sa, f.SVars)

	// The draw itself happened...
	if got := len(e.G.Zone(state.ZHand, 0)); got != drawsBefore+2 {
		t.Fatalf("CR 704.4 Weave Fate seq %d: drew %d, want 2 (hand %d -> %d)", start, got-drawsBefore, drawsBefore, got)
	}
	// ...but the creature carrying lethal damage is STILL on the battlefield:
	// no state-based action ran in the middle of the resolution. CR 704.4.
	if z := e.G.Obj(avenger).Zone; z != state.ZBattlefield {
		t.Fatalf("CR 704.4: Serra Avenger destroyed %s mid-resolution at seq %d; SBAs must wait until the resolution finishes", z, start)
	}

	// At the next priority boundary the deferred SBA fires and the 3/3 with
	// 3 lethal damage dies.
	e.checkStateBased()
	if z := e.G.Obj(avenger).Zone; z != state.ZGraveyard {
		t.Errorf("CR 704.5b: after the resolution and at the next boundary the 3/3 with 3 lethal damage must be in the graveyard (zone=%s)", z)
	}
}
