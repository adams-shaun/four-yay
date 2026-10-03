package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The Mimeoplasm's graveyard-to-exile setup (ticket: ChooseCard.ForgetChosen
// + ChangeZone-family ForgetOtherRemembered): a setup effect remembers two
// creatures, a ChangeZoneAll ForgetOtherRemembered$ True + RememberChanged$
// True batch replaces the remembered set with exactly the moved cards, and
// the follow-up ChooseCard ForgetChosen$ True drops the chosen card from the
// set while retaining the other -- whose power the chain's CounterNum$
// Remembered$CardPower then reads. These pins exercise the bookkeeping at
// its real choke points (effChooseCard's answered path, effChangeZone's
// object path, effChangeZoneAll's batch path) with inline fixtures; the
// end-to-end real-corpus regression lives in rules/mimeoplasm_forget_test.go.

func forgetFixtureHost(t *testing.T, names ...string) (*fakeHost, *state.Object, []*state.Object) {
	t.Helper()
	h := newHost(t, 2)
	src := h.g.AddObject(mkCard(t, "Name:Fixture Source\nTypes:Enchantment\nOracle:x\n"), 0)
	src.Zone = state.ZBattlefield
	h.g.SetZone(state.ZBattlefield, 0, []state.ObjID{src.ID})
	var ids []state.ObjID
	for _, n := range names {
		ids = append(ids, h.g.AddObject(mkCard(t, "Name:"+n+"\nTypes:Creature\nPT:2/2\nOracle:x\n"), 0).ID)
	}
	// Re-resolve after every AddObject: the returned *Object points into the
	// growing g.Objs backing array, so a pointer taken before a later append
	// is stale.
	src = h.g.Obj(src.ID)
	var cards []*state.Object
	for _, id := range ids {
		cards = append(cards, h.g.Obj(id))
	}
	return h, src, cards
}

func seedRemembered(h *fakeHost, src *state.Object, ids ...state.ObjID) {
	h.Emit(events.Event{Kind: events.Choose, Obj: src.ID, Counter: "remembered", IDs: ids})
}

func rememberedIDs(ts []state.Target) []state.ObjID {
	out := make([]state.ObjID, 0, len(ts))
	for _, t := range ts {
		out = append(out, t.Obj)
	}
	return out
}

func sameIDs(a, b []state.ObjID) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestChangeZoneAllForgetOtherRememberedReplacesTheSet: ChangeZoneAll with
// ForgetOtherRemembered$ True + RememberChanged$ True clears the prior
// remembered set BEFORE the sweep and re-remembers exactly the moved cards,
// in both halves -- the batch path of the Mimeoplasm's MimeoExile. A stale
// remembered object that the sweep does not move must be gone from the set.
func TestChangeZoneAllForgetOtherRememberedReplacesTheSet(t *testing.T) {
	h, src, cards := forgetFixtureHost(t, "First Creature", "Second Creature", "Stale Creature")
	first, second, stale := cards[0], cards[1], cards[2]
	h.g.SetZone(state.ZGraveyard, 0, []state.ObjID{first.ID, second.ID})
	first.Zone, second.Zone = state.ZGraveyard, state.ZGraveyard
	h.g.SetZone(state.ZExile, 0, []state.ObjID{stale.ID})
	stale.Zone = state.ZExile
	seedRemembered(h, src, first.ID, second.ID, stale.ID)

	// The Mimeoplasm's own MimeoExile shape.
	body := "DB$ ChangeZoneAll | Origin$ Graveyard | Destination$ Exile | ChangeType$ Card.IsRemembered | RememberChanged$ True | ForgetOtherRemembered$ True"
	sa := sa(t, body)
	if sa.API != "ChangeZoneAll" {
		t.Fatalf("precondition: fixture SA parsed as %q, want ChangeZoneAll", sa.API)
	}
	// Precondition: the sweep's Card.IsRemembered filter reads the
	// PERSISTENT list, so all three cards are candidates before the move.
	if got := rememberedIDs(h.g.Obj(src.ID).Remembered); len(got) != 3 {
		t.Fatalf("precondition: persistent remembered = %v, want all three", got)
	}

	c := &Ctx{Source: src.ID, Controller: 0,
		Remembered: []state.Target{{Obj: first.ID}, {Obj: second.ID}, {Obj: stale.ID}}}
	Resolve(h, c, sa)

	if got := rememberedIDs(c.Remembered); !sameIDs(got, []state.ObjID{first.ID, second.ID}) {
		t.Errorf("ctx remembered after the batch = %v, want exactly the moved pair", got)
	}
	if got := rememberedIDs(h.g.Obj(src.ID).Remembered); !sameIDs(got, []state.ObjID{first.ID, second.ID}) {
		t.Errorf("persistent remembered after the batch = %v, want exactly the moved pair", got)
	}
	for _, o := range []*state.Object{first, second} {
		if o.Zone != state.ZExile {
			t.Errorf("precondition violated by the run itself: %q zone = %s, want exile", o.Face().Name, o.Zone)
		}
	}
	if stale.Zone != state.ZExile {
		t.Errorf("the unmoved stale card was disturbed: zone = %s", stale.Zone)
	}
}

// TestChangeZoneForgetOtherRememberedReplacesTheSet: the same semantics on
// the ordinary ChangeZone object path (52 of the 67 corpus carriers): a
// Defined$ Remembered move whose ForgetOtherRemembered$ True rider clears
// the prior set before RememberChanged$ re-remembers exactly the moved
// cards. The Defined$ Remembered read still sees the pre-clear set (the
// clear sits after the read, beside the sibling ForgetOtherTargets$), so the
// move itself is not emptied by the forget.
func TestChangeZoneForgetOtherRememberedReplacesTheSet(t *testing.T) {
	h, src, cards := forgetFixtureHost(t, "First Creature", "Second Creature", "Stale Creature")
	first, second, stale := cards[0], cards[1], cards[2]
	h.g.SetZone(state.ZGraveyard, 0, []state.ObjID{first.ID, second.ID})
	first.Zone, second.Zone = state.ZGraveyard, state.ZGraveyard
	h.g.SetZone(state.ZExile, 0, []state.ObjID{stale.ID})
	stale.Zone = state.ZExile
	seedRemembered(h, src, first.ID, second.ID, stale.ID)

	sa := sa(t, "DB$ ChangeZone | Origin$ Graveyard | Destination$ Exile | Defined$ Remembered | RememberChanged$ True | ForgetOtherRemembered$ True")
	if sa.API != "ChangeZone" {
		t.Fatalf("precondition: fixture SA parsed as %q, want ChangeZone", sa.API)
	}
	if got := rememberedIDs(h.g.Obj(src.ID).Remembered); len(got) != 3 {
		t.Fatalf("precondition: persistent remembered = %v, want all three", got)
	}

	c := &Ctx{Source: src.ID, Controller: 0,
		Remembered: []state.Target{{Obj: first.ID}, {Obj: second.ID}, {Obj: stale.ID}}}
	Resolve(h, c, sa)

	if got := rememberedIDs(c.Remembered); !sameIDs(got, []state.ObjID{first.ID, second.ID}) {
		t.Errorf("ctx remembered after the move = %v, want exactly the moved pair", got)
	}
	if got := rememberedIDs(h.g.Obj(src.ID).Remembered); !sameIDs(got, []state.ObjID{first.ID, second.ID}) {
		t.Errorf("persistent remembered after the move = %v, want exactly the moved pair", got)
	}
	for _, o := range []*state.Object{first, second} {
		if o.Zone != state.ZExile {
			t.Errorf("%q was not moved: zone = %s, want exile", o.Face().Name, o.Zone)
		}
	}
	if stale.Zone != state.ZExile {
		t.Errorf("the unmoved stale card was disturbed: zone = %s", stale.Zone)
	}
}

// TestChooseCardChoicesFromPersistentRemembered pins the Mimeoplasm's ask.
func TestChooseCardChoicesFromPersistentRemembered(t *testing.T) {
	h, src, cards := forgetFixtureHost(t, "First Creature", "Second Creature")
	first, second := cards[0], cards[1]
	h.g.SetZone(state.ZExile, 0, []state.ObjID{first.ID, second.ID})
	first.Zone, second.Zone = state.ZExile, state.ZExile
	seedRemembered(h, src, first.ID, second.ID)

	sa := sa(t, "DB$ ChooseCard | Defined$ You | Amount$ 1 | Choices$ Creature.IsRemembered | ChoiceZone$ Exile")
	h.askResult = true
	effChooseCard(h, &Ctx{Source: src.ID, Controller: 0}, sa)
	d := h.lastAsk
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("precondition: no card ask was posed, got %+v", d)
	}
	seen := map[state.ObjID]bool{}
	for _, o := range d.Options {
		if o.Obj == first.ID {
			seen[first.ID] = true
		}
		if o.Obj == second.ID {
			seen[second.ID] = true
		}
	}
	if len(seen) != 2 {
		t.Errorf("the persistent remembered pair was not offered: offered options = %+v", d.Options)
	}
}
