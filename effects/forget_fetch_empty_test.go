package effects

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestChangeZoneHandOwnersForgetOtherRememberedOptionalConfirm covers the
// Optional$ confirmation boundary on the hidden-hand ChangeZone. Forge's
// ChangeZoneEffect.changeHiddenOriginResolve asks confirmAction BEFORE the
// card pick: a decline leaves the source's remembered cards untouched, while
// an accepted confirmation enters the fetch and clears them even when the
// following pick ends up empty. A Min-0 empty card selection is therefore an
// ACCEPT-then-pick-none, not a decline.
func TestChangeZoneHandOwnersForgetOtherRememberedOptionalConfirm(t *testing.T) {
	body := sa(t, "DB$ ChangeZone | Origin$ Hand | Destination$ Exile | DefinedPlayer$ You | ChangeType$ Creature.IsRemembered | ChangeNum$ 1 | Optional$ True | ForgetOtherRemembered$ True")

	setup := func(t *testing.T) (*fakeHost, *state.Object, *state.Object) {
		t.Helper()
		h, src, cards := forgetFixtureHost(t, "Remembered")
		card := h.g.Obj(cards[0].ID)
		h.g.SetZone(state.ZHand, 0, []state.ObjID{card.ID})
		card.Zone = state.ZHand
		seedRemembered(h, src, card.ID)
		if card.Zone != state.ZHand || !sameIDs(rememberedIDs(h.g.Obj(src.ID).Remembered), []state.ObjID{card.ID}) {
			t.Fatal("precondition: the remembered creature is in the asking player's hand and the persistent memory holds it")
		}
		return h, src, card
	}

	t.Run("decline keeps memory and does not pick", func(t *testing.T) {
		h, src, card := setup(t)
		c := &Ctx{Source: src.ID, Controller: 0, Remembered: []state.Target{{Obj: card.ID}}}
		h.askResult = true
		Resolve(h, c, body)
		confirm := h.lastAsk
		if confirm == nil || confirm.ResumeKind != "hand_move_confirm" || len(confirm.Options) != 2 ||
			confirm.Options[0].Kind != "yes" || confirm.Options[1].Kind != "no" {
			t.Fatalf("precondition: the optional hand move posted a yes/no confirmation, got %+v", confirm)
		}
		if !sameIDs(rememberedIDs(h.g.Obj(src.ID).Remembered), []state.ObjID{card.ID}) {
			t.Fatalf("precondition: the confirmation itself must not clear memory")
		}
		// Answer NO.
		c = &Ctx{Source: src.ID, Controller: 0, Remembered: confirm.ResumeRemembered,
			ForgetOtherSnapshot: confirm.ResumeForgetOtherSnapshot, ForgetOtherOwners: confirm.ResumeForgetOtherOwners,
			ForgetOtherReady: confirm.ResumeForgetOtherReady, ForgetOtherCleared: confirm.ResumeForgetOtherCleared,
			HandMoveConfirmDone: true, HandMoveConfirm: "no", HandMoveConfirmTarget: 0}
		h.lastAsk = nil
		Resolve(h, c, body)
		if h.lastAsk != nil {
			t.Fatalf("a declined confirmation must not post a card pick, got %+v", h.lastAsk)
		}
		if card.Zone != state.ZHand {
			t.Fatalf("declined card moved: zone=%s", card.Zone)
		}
		if got := rememberedIDs(h.g.Obj(src.ID).Remembered); !sameIDs(got, []state.ObjID{card.ID}) {
			t.Errorf("decline cleared remembered state: %v, want [%d]", got, card.ID)
		}
		if !sameIDs(rememberedIDs(c.Remembered), []state.ObjID{card.ID}) {
			t.Errorf("decline cleared resolution-local remembered state: %v", c.Remembered)
		}
	})

	t.Run("accepted pick-none clears", func(t *testing.T) {
		h, src, card := setup(t)
		c := &Ctx{Source: src.ID, Controller: 0, Remembered: []state.Target{{Obj: card.ID}}}
		h.askResult = true
		Resolve(h, c, body)
		confirm := h.lastAsk
		if confirm == nil || confirm.ResumeKind != "hand_move_confirm" {
			t.Fatalf("precondition: the optional hand move posted a confirmation, got %+v", confirm)
		}
		// Answer YES.
		c = &Ctx{Source: src.ID, Controller: 0, Remembered: confirm.ResumeRemembered,
			ForgetOtherSnapshot: confirm.ResumeForgetOtherSnapshot, ForgetOtherOwners: confirm.ResumeForgetOtherOwners,
			ForgetOtherReady: confirm.ResumeForgetOtherReady, ForgetOtherCleared: confirm.ResumeForgetOtherCleared,
			HandMoveConfirmDone: true, HandMoveConfirm: "yes", HandMoveConfirmTarget: 0}
		Resolve(h, c, body)
		pick := h.lastAsk
		if pick == nil || pick.ResumeKind != "hand_move" || len(pick.Options) != 1 || pick.Options[0].Obj != card.ID {
			t.Fatalf("accepted confirmation did not post the card pick: %+v", pick)
		}
		if got := rememberedIDs(h.g.Obj(src.ID).Remembered); len(got) != 0 {
			t.Fatalf("accepted confirmation must clear before the pick: persistent=%v", got)
		}
		// Answer with NO card (the Min-0 pick-none).
		c = &Ctx{Source: src.ID, Controller: 0, Remembered: pick.ResumeRemembered,
			ForgetOtherSnapshot: pick.ResumeForgetOtherSnapshot, ForgetOtherOwners: pick.ResumeForgetOtherOwners,
			ForgetOtherReady: pick.ResumeForgetOtherReady, ForgetOtherCleared: pick.ResumeForgetOtherCleared,
			HandMoveDone: true, HandMoveTarget: 0}
		Resolve(h, c, body)
		if card.Zone != state.ZHand {
			t.Fatalf("pick-none must not move the card: zone=%s", card.Zone)
		}
		if got := rememberedIDs(h.g.Obj(src.ID).Remembered); len(got) != 0 {
			t.Errorf("accepted pick-none kept persistent remembered state: %v", got)
		}
		if len(c.Remembered) != 0 {
			t.Errorf("accepted pick-none kept resolution-local remembered state: %v", c.Remembered)
		}
	})

	t.Run("accepted pick clears", func(t *testing.T) {
		h, src, card := setup(t)
		c := &Ctx{Source: src.ID, Controller: 0, Remembered: []state.Target{{Obj: card.ID}}}
		h.askResult = true
		Resolve(h, c, body)
		confirm := h.lastAsk
		if confirm == nil || confirm.ResumeKind != "hand_move_confirm" {
			t.Fatalf("precondition: the optional hand move posted a confirmation, got %+v", confirm)
		}
		c = &Ctx{Source: src.ID, Controller: 0, Remembered: confirm.ResumeRemembered,
			ForgetOtherSnapshot: confirm.ResumeForgetOtherSnapshot, ForgetOtherOwners: confirm.ResumeForgetOtherOwners,
			ForgetOtherReady: confirm.ResumeForgetOtherReady, ForgetOtherCleared: confirm.ResumeForgetOtherCleared,
			HandMoveConfirmDone: true, HandMoveConfirm: "yes", HandMoveConfirmTarget: 0}
		Resolve(h, c, body)
		pick := h.lastAsk
		if pick == nil || pick.ResumeKind != "hand_move" || pick.Options[0].Obj != card.ID {
			t.Fatalf("accepted confirmation did not post the card pick: %+v", pick)
		}
		c = &Ctx{Source: src.ID, Controller: 0, Remembered: pick.ResumeRemembered,
			ForgetOtherSnapshot: pick.ResumeForgetOtherSnapshot, ForgetOtherOwners: pick.ResumeForgetOtherOwners,
			ForgetOtherReady: pick.ResumeForgetOtherReady, ForgetOtherCleared: pick.ResumeForgetOtherCleared,
			HandMove: []state.ObjID{card.ID}, HandMoveDone: true, HandMoveTarget: 0}
		Resolve(h, c, body)
		if card.Zone != state.ZExile {
			t.Fatalf("accepted card did not move: zone=%s", card.Zone)
		}
		if got := rememberedIDs(h.g.Obj(src.ID).Remembered); len(got) != 0 {
			t.Errorf("accepted move kept persistent remembered state: %v", got)
		}
	})
}

// TestChangeZoneForgetOtherRememberedFetchEmpty is the empty-pool boundary for
// the hidden-hand ChangeZone: an Optional$ fetch whose pool is empty still asks
// whether to proceed (acceptance clears, decline retains), and a mandatory
// fetch with an empty pool clears without moving. The formerly remembered card
// keeps matching the Card.IsRemembered filter across the clear, which is what
// makes the optional acceptance observable at all.
func TestChangeZoneForgetOtherRememberedFetchEmpty(t *testing.T) {
	optionalBody := sa(t, "DB$ ChangeZone | Origin$ Hand | Destination$ Exile | DefinedPlayer$ You | ChangeType$ Creature.IsRemembered | ChangeNum$ 1 | Optional$ True | ForgetOtherRemembered$ True")
	mandatoryBody := sa(t, "DB$ ChangeZone | Origin$ Hand | Destination$ Exile | DefinedPlayer$ You | ChangeType$ Creature.IsRemembered | ChangeNum$ 1 | Mandatory$ True | ForgetOtherRemembered$ True")

	setup := func(t *testing.T) (*fakeHost, *state.Object, *state.Object) {
		t.Helper()
		h, src, cards := forgetFixtureHost(t, "Remembered")
		remembered := h.g.Obj(cards[0].ID)
		other := h.g.AddObject(mkCard(t, "Name:Not Remembered\nTypes:Creature\nPT:1/1\nOracle:x\n"), 0)
		remembered, other = h.g.Obj(cards[0].ID), h.g.Obj(other.ID)
		// The remembered card is NOT in the hand, so the pool is empty.
		h.g.SetZone(state.ZExile, 0, []state.ObjID{remembered.ID})
		remembered.Zone = state.ZExile
		h.g.SetZone(state.ZHand, 0, []state.ObjID{other.ID})
		other.Zone = state.ZHand
		seedRemembered(h, src, remembered.ID)
		ctx := &Ctx{Source: src.ID, Controller: 0, Remembered: []state.Target{{Obj: remembered.ID}}}
		if !MatchesSpecCtx(h.g, "Creature.IsRemembered", remembered.ID, ctx.SpecContext(0)) {
			t.Fatal("precondition: the seeded remembered card matches the filter")
		}
		if MatchesSpecCtx(h.g, "Creature.IsRemembered", other.ID, ctx.SpecContext(0)) {
			t.Fatal("precondition: the hand card must NOT match the remembered filter")
		}
		if !sameIDs(rememberedIDs(h.g.Obj(src.ID).Remembered), []state.ObjID{remembered.ID}) {
			t.Fatal("precondition: the persistent memory holds exactly the remembered card")
		}
		return h, src, remembered
	}

	t.Run("optional empty asks and decline keeps", func(t *testing.T) {
		h, src, remembered := setup(t)
		c := &Ctx{Source: src.ID, Controller: 0, Remembered: []state.Target{{Obj: remembered.ID}}}
		h.askResult = true
		Resolve(h, c, optionalBody)
		confirm := h.lastAsk
		if confirm == nil || confirm.ResumeKind != "hand_move_confirm" {
			t.Fatalf("optional empty pool must ask whether to proceed, got %+v", confirm)
		}
		c = &Ctx{Source: src.ID, Controller: 0, Remembered: confirm.ResumeRemembered,
			ForgetOtherSnapshot: confirm.ResumeForgetOtherSnapshot, ForgetOtherOwners: confirm.ResumeForgetOtherOwners,
			ForgetOtherReady: confirm.ResumeForgetOtherReady, ForgetOtherCleared: confirm.ResumeForgetOtherCleared,
			HandMoveConfirmDone: true, HandMoveConfirm: "no", HandMoveConfirmTarget: 0}
		Resolve(h, c, optionalBody)
		if got := rememberedIDs(h.g.Obj(src.ID).Remembered); !sameIDs(got, []state.ObjID{remembered.ID}) {
			t.Errorf("optional empty decline cleared memory: %v, want [%d]", got, remembered.ID)
		}
	})

	t.Run("optional empty accept clears", func(t *testing.T) {
		h, src, remembered := setup(t)
		c := &Ctx{Source: src.ID, Controller: 0, Remembered: []state.Target{{Obj: remembered.ID}}}
		h.askResult = true
		Resolve(h, c, optionalBody)
		confirm := h.lastAsk
		if confirm == nil || confirm.ResumeKind != "hand_move_confirm" {
			t.Fatalf("optional empty pool must ask whether to proceed, got %+v", confirm)
		}
		c = &Ctx{Source: src.ID, Controller: 0, Remembered: confirm.ResumeRemembered,
			ForgetOtherSnapshot: confirm.ResumeForgetOtherSnapshot, ForgetOtherOwners: confirm.ResumeForgetOtherOwners,
			ForgetOtherReady: confirm.ResumeForgetOtherReady, ForgetOtherCleared: confirm.ResumeForgetOtherCleared,
			HandMoveConfirmDone: true, HandMoveConfirm: "yes", HandMoveConfirmTarget: 0}
		h.lastAsk = nil
		Resolve(h, c, optionalBody)
		if h.lastAsk != nil {
			t.Fatalf("optional empty accept must not post a card pick, got %+v", h.lastAsk)
		}
		if got := rememberedIDs(h.g.Obj(src.ID).Remembered); len(got) != 0 {
			t.Errorf("optional empty accept kept persistent remembered state: %v", got)
		}
		if len(c.Remembered) != 0 {
			t.Errorf("optional empty accept kept resolution-local remembered state: %v", c.Remembered)
		}
	})

	t.Run("mandatory empty clears", func(t *testing.T) {
		h, src, remembered := setup(t)
		c := &Ctx{Source: src.ID, Controller: 0, Remembered: []state.Target{{Obj: remembered.ID}}}
		h.askResult = true
		Resolve(h, c, mandatoryBody)
		if h.lastAsk != nil {
			t.Fatalf("mandatory empty pool must not post a decision, got %+v", h.lastAsk)
		}
		if got := rememberedIDs(h.g.Obj(src.ID).Remembered); len(got) != 0 {
			t.Errorf("mandatory empty pool kept persistent remembered state: %v", got)
		}
		if len(c.Remembered) != 0 {
			t.Errorf("mandatory empty pool kept resolution-local remembered state: %v", c.Remembered)
		}
		for _, ev := range h.log {
			if ev.Kind == events.Note && strings.Contains(ev.Text, "unimplemented API ChangeZone") {
				t.Fatal("precondition: the ChangeZone handler ran, not the unimplemented-API fallback")
			}
		}
	})
}

// TestChangeZoneHiddenForgetOtherRememberedFetchEmpty pins the hidden public-
// origin pick's no-move boundary: a Hidden$ fetch whose pool is empty still
// clears the source's remembered cards, because the fetch was entered.
func TestChangeZoneHiddenForgetOtherRememberedFetchEmpty(t *testing.T) {
	body := sa(t, "DB$ ChangeZone | Hidden$ True | Origin$ Graveyard | Destination$ Exile | ChangeType$ Creature.IsRemembered | ChangeNum$ 1 | Mandatory$ True | ForgetOtherRemembered$ True")
	h, src, cards := forgetFixtureHost(t, "Remembered")
	remembered := h.g.Obj(cards[0].ID)
	// The remembered card is in EXILE, not the graveyard, so the graveyard pool
	// is empty.
	h.g.SetZone(state.ZExile, 0, []state.ObjID{remembered.ID})
	remembered.Zone = state.ZExile
	seedRemembered(h, src, remembered.ID)
	ctx := &Ctx{Source: src.ID, Controller: 0, Remembered: []state.Target{{Obj: remembered.ID}}}
	if !MatchesSpecCtx(h.g, "Creature.IsRemembered", remembered.ID, ctx.SpecContext(0)) {
		t.Fatal("precondition: the seeded remembered card matches the filter")
	}
	if len(h.g.Zone(state.ZGraveyard, 0)) != 0 {
		t.Fatal("precondition: the graveyard pool is empty")
	}
	h.askResult = true
	Resolve(h, ctx, body)
	if h.lastAsk != nil {
		t.Fatalf("empty hidden pool must not post a decision, got %+v", h.lastAsk)
	}
	if got := rememberedIDs(h.g.Obj(src.ID).Remembered); len(got) != 0 {
		t.Errorf("empty hidden fetch kept persistent remembered state: %v", got)
	}
}

// TestChangeZoneSearchForgetOtherRememberedFetchEmpty pins the library search's
// no-move boundary: a search whose pool is empty still clears the source's
// remembered cards.
func TestChangeZoneSearchForgetOtherRememberedFetchEmpty(t *testing.T) {
	body := sa(t, "DB$ ChangeZone | Origin$ Library | Destination$ Hand | ChangeType$ Creature.IsRemembered | ChangeNum$ 1 | Mandatory$ True | ForgetOtherRemembered$ True")
	h, src, cards := forgetFixtureHost(t, "Remembered")
	remembered := h.g.Obj(cards[0].ID)
	h.g.SetZone(state.ZExile, 0, []state.ObjID{remembered.ID})
	remembered.Zone = state.ZExile
	seedRemembered(h, src, remembered.ID)
	ctx := &Ctx{Source: src.ID, Controller: 0, Remembered: []state.Target{{Obj: remembered.ID}}}
	if !MatchesSpecCtx(h.g, "Creature.IsRemembered", remembered.ID, ctx.SpecContext(0)) {
		t.Fatal("precondition: the seeded remembered card matches the filter")
	}
	if len(h.g.Zone(state.ZLibrary, 0)) != 0 {
		t.Fatal("precondition: the searched library is empty")
	}
	h.askResult = true
	Resolve(h, ctx, body)
	if got := rememberedIDs(h.g.Obj(src.ID).Remembered); len(got) != 0 {
		t.Errorf("empty library search kept persistent remembered state: %v", got)
	}
}

// TestChangeZoneDefinedForgetOtherRememberedFetchEmpty pins the Optional$
// object-valued Defined$ library fetch boundary (Forge's
// ChangeZoneEffect.confirmAction then clearRemembered): accepting a fetch that
// finds nothing clears the remembered set, declining it does not.
func TestChangeZoneDefinedForgetOtherRememberedFetchEmpty(t *testing.T) {
	body := sa(t, "DB$ ChangeZone | Origin$ Library | Destination$ Battlefield | Defined$ Remembered | Optional$ True | ForgetOtherRemembered$ True")

	setup := func(t *testing.T) (*fakeHost, *state.Object, *state.Object) {
		t.Helper()
		h, src, cards := forgetFixtureHost(t, "Remembered")
		remembered := h.g.Obj(cards[0].ID)
		// The remembered card is in EXILE, not the library, so the defined
		// fetch list is a nonempty object list that MOVE-rechecks to nothing.
		h.g.SetZone(state.ZExile, 0, []state.ObjID{remembered.ID})
		remembered.Zone = state.ZExile
		seedRemembered(h, src, remembered.ID)
		if !sameIDs(rememberedIDs(h.g.Obj(src.ID).Remembered), []state.ObjID{remembered.ID}) {
			t.Fatal("precondition: the persistent memory holds exactly the remembered card")
		}
		return h, src, remembered
	}

	t.Run("decline keeps", func(t *testing.T) {
		h, src, remembered := setup(t)
		c := &Ctx{Source: src.ID, Controller: 0, Remembered: []state.Target{{Obj: remembered.ID}}}
		h.askResult = true
		Resolve(h, c, body)
		if h.lastAsk == nil || h.lastAsk.ResumeKind != "defined_library_optional" {
			t.Fatalf("precondition: the optional defined fetch posted a yes/no, got %+v", h.lastAsk)
		}
		c = &Ctx{Source: src.ID, Controller: 0, Remembered: c.Remembered, DefinedLibraryMove: "no"}
		Resolve(h, c, body)
		if got := rememberedIDs(h.g.Obj(src.ID).Remembered); !sameIDs(got, []state.ObjID{remembered.ID}) {
			t.Errorf("declined defined fetch cleared memory: %v, want [%d]", got, remembered.ID)
		}
	})

	t.Run("accept clears with no move", func(t *testing.T) {
		h, src, remembered := setup(t)
		c := &Ctx{Source: src.ID, Controller: 0, Remembered: []state.Target{{Obj: remembered.ID}}}
		h.askResult = true
		Resolve(h, c, body)
		if h.lastAsk == nil || h.lastAsk.ResumeKind != "defined_library_optional" {
			t.Fatalf("precondition: the optional defined fetch posted a yes/no, got %+v", h.lastAsk)
		}
		c = &Ctx{Source: src.ID, Controller: 0, Remembered: c.Remembered, DefinedLibraryMove: "yes"}
		Resolve(h, c, body)
		if remembered.Zone != state.ZExile {
			t.Fatalf("accepted empty defined fetch must not move the card: zone=%s", remembered.Zone)
		}
		if got := rememberedIDs(h.g.Obj(src.ID).Remembered); len(got) != 0 {
			t.Errorf("accepted defined fetch kept persistent remembered state: %v", got)
		}
	})
}
