package effects

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestChangeZoneHandOwnersForgetOtherRememberedNoEligible is the mandatory
// empty-pool regression: a mandatory hidden-hand fetch that finds no matching
// card still ENTERED the fetch, so it clears the persistent and local
// remembered set without moving a card (Forge clears at
// ChangeZoneEffect.changeHiddenOriginResolve 1103 before the choose, and does
// not require a nonempty fetchList). The baseline sub-run has an eligible
// remembered card, which is asked and whose move clears -- so a green
// no-eligible run cannot be the primitive silently doing nothing.
func TestChangeZoneHandOwnersForgetOtherRememberedNoEligible(t *testing.T) {
	body := sa(t, "DB$ ChangeZone | Origin$ Hand | Destination$ Exile | DefinedPlayer$ You | ChangeType$ Creature.IsRemembered | ChangeNum$ 1 | Mandatory$ True | ForgetOtherRemembered$ True")

	baseRun := func(t *testing.T, rememberedInHand bool) (*fakeHost, *state.Object, *state.Object) {
		t.Helper()
		h, src, cards := forgetFixtureHost(t, "Remembered")
		remembered := h.g.Obj(cards[0].ID)
		other := h.g.AddObject(mkCard(t, "Name:Not Remembered\nTypes:Creature\nPT:1/1\nOracle:x\n"), 0)
		// Re-resolve after the AddObject: the earlier pointer predates the
		// g.Objs append and is stale (the fixture helper's own warning).
		remembered, other = h.g.Obj(cards[0].ID), h.g.Obj(other.ID)
		zone := state.ZExile
		hand := []state.ObjID{other.ID}
		if rememberedInHand {
			zone = state.ZHand
			hand = []state.ObjID{remembered.ID, other.ID}
		}
		h.g.SetZone(zone, 0, []state.ObjID{remembered.ID})
		remembered.Zone = zone
		h.g.SetZone(state.ZHand, 0, hand)
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

	t.Run("baseline eligible taken clears", func(t *testing.T) {
		h, src, remembered := baseRun(t, true)
		// A mandatory move whose pool fits the count takes every eligible card
		// deterministically, with no ask; the move must clear the memory.
		c := &Ctx{Source: src.ID, Controller: 0, Remembered: []state.Target{{Obj: remembered.ID}}}
		h.askResult = true
		Resolve(h, c, body)
		if h.lastAsk != nil {
			t.Fatalf("baseline: a required take-all must not post a decision, got %+v", h.lastAsk)
		}
		if remembered.Zone != state.ZExile {
			t.Fatalf("baseline eligible card did not move: zone=%s", remembered.Zone)
		}
		if got := rememberedIDs(h.g.Obj(src.ID).Remembered); len(got) != 0 {
			t.Errorf("baseline move did not clear remembered state: %v", got)
		}
	})

	t.Run("mandatory no eligible clears", func(t *testing.T) {
		h, src, remembered := baseRun(t, false)
		c := &Ctx{Source: src.ID, Controller: 0, Remembered: []state.Target{{Obj: remembered.ID}}}
		h.askResult = true
		Resolve(h, c, body)
		if h.lastAsk != nil {
			t.Fatalf("mandatory no-eligible hand must not post a decision, got %+v", h.lastAsk)
		}
		if remembered.Zone != state.ZExile {
			t.Fatalf("precondition failed: the remembered card must stay put, zone=%s", remembered.Zone)
		}
		if got := rememberedIDs(h.g.Obj(src.ID).Remembered); len(got) != 0 {
			t.Errorf("mandatory no-eligible path kept remembered state: %v, want empty", got)
		}
		if len(c.Remembered) != 0 {
			t.Errorf("mandatory no-eligible path kept resolution-local remembered state: %v", c.Remembered)
		}
		// The fetch handler must have RUN, not fallen through to the
		// unimplemented-API Note (a green run cannot be the primitive being
		// unregistered).
		for _, ev := range h.log {
			if ev.Kind == events.Note && strings.Contains(ev.Text, "unimplemented API ChangeZone") {
				t.Fatal("precondition: the ChangeZone handler ran, not the unimplemented-API fallback")
			}
		}
	})
}
