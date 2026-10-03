package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

type tapCaptureHost struct {
	*fakeHost
	tapper state.PlayerID
}

func (h *tapCaptureHost) EmitTap(obj state.ObjID, tapper state.PlayerID, entering bool) {
	h.tapper = tapper
	h.fakeHost.EmitTap(obj, tapper, entering)
}

func battlefieldRememberedConsumer(t *testing.T) (*tapCaptureHost, *Ctx, *state.Object) {
	t.Helper()
	base, c, _ := mixedRememberedHost(t)
	card := base.g.AddObject(mkCard(t, "Name:Tap Target\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"), 0)
	base.g.SetZone(state.ZBattlefield, 0, []state.ObjID{card.ID})
	card.Zone = state.ZBattlefield
	c.Source = card.ID
	if card.Zone != state.ZBattlefield {
		t.Fatal("precondition: tap target is not on the battlefield")
	}
	return &tapCaptureHost{fakeHost: base}, c, card
}

func TestTapTapperPlainRememberedExcludesCardController(t *testing.T) {
	h, c, target := battlefieldRememberedConsumer(t)
	if target.Controller == c.Remembered[1].Player {
		t.Fatal("precondition: remembered card controller and remembered player must differ")
	}
	effTap(h, c, &cards.SA{Params: map[string]string{"Defined": "Self", "Tapper": "Remembered"}})
	if h.tapper != 2 {
		t.Fatalf("effTap tapper = %d, want remembered player 2 (not card controller 1)", h.tapper)
	}
	if !target.Tapped {
		t.Fatal("precondition/result: effTap did not tap the battlefield target")
	}
}

func TestPlayControllerPlainRememberedExcludesCardController(t *testing.T) {
	h, c, _ := battlefieldRememberedConsumer(t)
	// The remembered card must actually be in the public zone from which this
	// effect gathers candidates.
	rememberedCard := h.g.Obj(c.Remembered[0].Obj)
	rememberedCard.Zone = state.ZGraveyard
	h.g.SetZone(state.ZGraveyard, rememberedCard.Owner, []state.ObjID{rememberedCard.ID})
	if rememberedCard.Zone != state.ZGraveyard || rememberedCard.Controller != 1 {
		t.Fatalf("precondition: remembered Play card zone/controller = %v/%d", rememberedCard.Zone, rememberedCard.Controller)
	}
	sa := &cards.SA{Params: map[string]string{"Defined": "Remembered", "Controller": "Remembered"}}
	effPlay(h, c, sa)
	if h.lastAsk == nil {
		t.Fatal("precondition: Play did not reach its player decision")
	}
	if h.lastAsk.Player != 2 {
		t.Fatalf("Play decision player = %d, want remembered player 2 (not card controller 1)", h.lastAsk.Player)
	}
}

func TestBecomeMonarchPlainRememberedExcludesCardController(t *testing.T) {
	h, c, _ := mixedRememberedHost(t)
	if c.Remembered[0].IsPlayer || c.Remembered[1].Player == 1 {
		t.Fatal("precondition: mixed remembered card/player seats are not distinct")
	}
	effBecomeMonarch(h, c, &cards.SA{Params: map[string]string{"Defined": "Remembered"}})
	var got []state.PlayerID
	for _, e := range h.log {
		if e.Kind == events.MonarchChange {
			got = append(got, e.Player)
		}
	}
	if len(got) != 1 || got[0] != 2 {
		t.Fatalf("MonarchChange players = %v, want [2] (remembered player, not card controller)", got)
	}
	if !h.g.IsMonarch(2) || h.g.IsMonarch(1) {
		t.Fatalf("monarch state: seat 2=%v seat 1=%v, want only seat 2", h.g.IsMonarch(2), h.g.IsMonarch(1))
	}
}
