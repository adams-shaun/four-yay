package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

type placerCaptureHost struct {
	askHost
	adders []state.PlayerID
}

func (h *placerCaptureHost) Emit(ev events.Event) {
	if ev.Kind == events.CounterChange {
		h.adders = append(h.adders, h.counterAdder-1)
	}
	h.fakeHost.Emit(ev)
}

func TestPutCounterPlayerIsRememberedPlacer(t *testing.T) {
	h := &placerCaptureHost{}
	h.g = state.NewGame(names(2))
	target := h.g.AddObject(vowBear(t, "Remembered player's bear"), 0)
	target.Zone = state.ZBattlefield
	h.g.SetZone(state.ZBattlefield, 0, []state.ObjID{target.ID})
	ctx := &Ctx{Source: target.ID, Controller: 1,
		Remembered: []state.Target{{Player: 0, IsPlayer: true}}}
	if h.g.Obj(target.ID).Zone != state.ZBattlefield || len(ctx.Remembered) != 1 || ctx.Remembered[0].Player == ctx.Controller {
		t.Fatal("precondition: remembered player must be distinct from controller and target must be on battlefield")
	}
	Resolve(h, ctx, sa(t, "DB$ PutCounter | Defined$ Self | CounterType$ P1P1 | CounterNum$ 1 | Placer$ Player.IsRemembered"))
	if got := h.g.Obj(target.ID).Counter("P1P1"); got != 1 {
		t.Fatalf("target P1P1 counters = %d, want 1; events=%+v", got, h.log)
	}
	if len(h.adders) != 1 || h.adders[0] != 0 {
		t.Fatalf("counter adder captures = %v, want remembered player 0 (not controller 1)", h.adders)
	}
	if h.counterAdder != 0 {
		t.Fatalf("counter adder publication leaked after emit: %d", h.counterAdder)
	}
}
