package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

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
