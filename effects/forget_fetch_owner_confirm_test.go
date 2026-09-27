package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

func TestChangeZoneHandOwnersForgetOtherRememberedOptionalConfirmPerOwner(t *testing.T) {
	h, src, cards := forgetFixtureHost(t, "Owner zero")
	first := h.g.Obj(cards[0].ID)
	second := h.g.AddObject(mkCard(t, "Name:Owner one\nTypes:Creature\nPT:2/2\nOracle:x\n"), 1)
	second = h.g.Obj(second.ID)
	h.g.SetZone(state.ZExile, 0, []state.ObjID{first.ID})
	h.g.SetZone(state.ZExile, 1, []state.ObjID{second.ID})
	first.Zone, second.Zone = state.ZExile, state.ZExile
	seedRemembered(h, src, first.ID, second.ID)
	body := sa(t, "DB$ ChangeZone | Origin$ Hand | Destination$ Exile | DefinedPlayer$ RememberedOwner | ChangeType$ Creature.IsRemembered | ChangeNum$ 1 | Optional$ True | ForgetOtherRemembered$ True")
	ctx := &Ctx{Source: src.ID, Controller: 0, Remembered: []state.Target{{Obj: first.ID}, {Obj: second.ID}}}
	if first.ID == second.ID || first.Owner == second.Owner || first.Zone != state.ZExile || second.Zone != state.ZExile {
		t.Fatal("precondition: distinct remembered cards owned by different players are outside both hands")
	}
	if len(h.g.Zone(state.ZHand, 0)) != 0 || len(h.g.Zone(state.ZHand, 1)) != 0 {
		t.Fatal("precondition: both optional fetch pools are empty")
	}
	h.askResult = true
	Resolve(h, ctx, body)
	firstAsk := h.lastAsk
	if firstAsk == nil || firstAsk.ResumeKind != "hand_move_confirm" || firstAsk.Player != 0 {
		t.Fatalf("precondition: first owner must receive the first confirmation: %+v", firstAsk)
	}

	// Declining owner zero must preserve memory, but must not consume owner one's
	// distinct confirmation.
	ctx = &Ctx{Source: src.ID, Controller: 0, Remembered: firstAsk.ResumeRemembered,
		ForgetOtherSnapshot: firstAsk.ResumeForgetOtherSnapshot, ForgetOtherOwners: firstAsk.ResumeForgetOtherOwners,
		ForgetOtherReady: firstAsk.ResumeForgetOtherReady, ForgetOtherCleared: firstAsk.ResumeForgetOtherCleared,
		HandMoveConfirmDone: true, HandMoveConfirm: "no", HandMoveConfirmTarget: firstAsk.ResumeTarget}
	Resolve(h, ctx, body)
	secondAsk := h.lastAsk
	if secondAsk == nil || secondAsk.ResumeKind != "hand_move_confirm" || secondAsk.Player != 1 {
		t.Fatalf("owner one confirmation was skipped after owner zero declined: %+v", secondAsk)
	}
	if got := rememberedIDs(h.g.Obj(src.ID).Remembered); !sameIDs(got, []state.ObjID{first.ID, second.ID}) {
		t.Fatalf("owner zero decline cleared memory before owner one's choice: %v", got)
	}

	ctx = &Ctx{Source: src.ID, Controller: 0, Remembered: secondAsk.ResumeRemembered,
		ForgetOtherSnapshot: secondAsk.ResumeForgetOtherSnapshot, ForgetOtherOwners: secondAsk.ResumeForgetOtherOwners,
		ForgetOtherReady: secondAsk.ResumeForgetOtherReady, ForgetOtherCleared: secondAsk.ResumeForgetOtherCleared,
		HandMoveConfirmDone: true, HandMoveConfirm: "yes", HandMoveConfirmTarget: secondAsk.ResumeTarget}
	h.lastAsk = nil
	Resolve(h, ctx, body)
	if h.lastAsk != nil {
		t.Fatalf("accepted empty owner-one fetch must finish without a card pick: %+v", h.lastAsk)
	}
	if got := rememberedIDs(h.g.Obj(src.ID).Remembered); len(got) != 0 {
		t.Errorf("accepted owner-one fetch kept persistent remembered state: %v", got)
	}
	if len(ctx.Remembered) != 0 {
		t.Errorf("accepted owner-one fetch kept local remembered state: %v", ctx.Remembered)
	}
	if first.Zone != state.ZExile || second.Zone != state.ZExile {
		t.Errorf("empty fetch moved a card: zones %s, %s", first.Zone, second.Zone)
	}
}
