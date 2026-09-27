package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The ForgetOtherRemembered$ continuation leaves (ticket: preserve
// IsRemembered selectors across a suspended ForgetOtherRemembered). The
// synchronous leaves are forget_other_remembered_test.go's; these pin the
// ASKED passes: a walk whose first pass clears the remembered set and then
// suspends on a player answer resumes in a FRESH Ctx, so only the ask's
// own ride (Decision.ResumeForgetOther*) can carry the pre-clear candidates
// across. Each test drives a first pass to its ask, answers it by
// reconstructing the resumed Ctx the way rules' resumeResolution does
// (answers + the ask's ride fields), and asserts the pre-clear selector
// still matched through re-entry. The clear-remembered Choose event must be
// emitted exactly once per effect despite the suspensions.

// clearRememberedCount counts the source's clear-remembered Choose events
// in the fixture host's log.
func clearRememberedCount(h *fakeHost, src state.ObjID) int {
	n := 0
	for _, e := range h.log {
		if e.Kind == events.Choose && e.Counter == "clear-remembered" && e.Obj == src {
			n++
		}
	}
	return n
}

// forgetRide is the shared fresh-Ctx reconstruction of one answered ask: the
// answers the resume arm sets plus the ask's own ForgetOtherRemembered$
// ride, exactly what rules/resolution.go's rebuild carries.
type forgetRide struct {
	d *decision.Decision
}

func (r forgetRide) ctx(base *Ctx) *Ctx {
	c := base
	c.Remembered = append([]state.Target(nil), r.d.ResumeRemembered...)
	c.Chosen = append([]state.Target(nil), r.d.ResumeChoices...)
	c.ChosenValid = r.d.ResumeChosenValid
	c.ForgetOtherSnapshot = append([]state.Target(nil), r.d.ResumeForgetOtherSnapshot...)
	c.ForgetOtherOwners = append([]state.PlayerID(nil), r.d.ResumeForgetOtherOwners...)
	c.ForgetOtherReady = r.d.ResumeForgetOtherReady
	c.ForgetOtherCleared = r.d.ResumeForgetOtherCleared
	return c
}

// The owner-SELECTED hand walk (effChangeZoneHandOwners, DefinedPlayer$
// RememberedOwner): owner A's answer settles and CLEARS the remembered set,
// owner B's ask must still offer B's pre-clear candidates, and B's answered
// revalidation must admit B's candidate through a fresh Ctx.
func TestHandMoveForgetOtherRememberedOwnersAsk(t *testing.T) {
	h, src, cards := forgetFixtureHost(t, "First Creature", "Second Creature", "Stale Creature")
	first, second, stale := cards[0], cards[1], cards[2]
	extraA := h.g.Obj(h.g.AddObject(mkCard(t, "Name:Extra A\nTypes:Creature\nPT:2/2\nOracle:x\n"), 0).ID)
	extraB := h.g.Obj(h.g.AddObject(mkCard(t, "Name:Extra B\nTypes:Creature\nPT:2/2\nOracle:x\n"), 1).ID)
	// Re-resolve after the AddObject calls: the fixture's pointers are into
	// the growing g.Objs backing array and went stale above.
	first, second, stale = h.g.Obj(first.ID), h.g.Obj(second.ID), h.g.Obj(stale.ID)
	h.g.SetZone(state.ZHand, 0, []state.ObjID{first.ID, extraA.ID})
	first.Zone, extraA.Zone = state.ZHand, state.ZHand
	h.g.SetZone(state.ZHand, 1, []state.ObjID{second.ID, extraB.ID})
	second.Zone, extraB.Zone = state.ZHand, state.ZHand
	h.g.SetZone(state.ZExile, 0, []state.ObjID{stale.ID})
	stale.Zone = state.ZExile
	seedRemembered(h, src, first.ID, extraA.ID, second.ID, extraB.ID, stale.ID)
	body := sa(t, "DB$ ChangeZone | Origin$ Hand | Destination$ Exile | DefinedPlayer$ RememberedOwner | ChangeType$ Creature.IsRemembered | ChangeNum$ 1 | Mandatory$ True | RememberChanged$ True | ForgetOtherRemembered$ True")
	if first.ID == second.ID || first.Zone != state.ZHand || second.Zone != state.ZHand ||
		!sameIDs(rememberedIDs(h.g.Obj(src.ID).Remembered), []state.ObjID{first.ID, extraA.ID, second.ID, extraB.ID, stale.ID}) {
		t.Fatal("precondition: two remembered creatures per owner's hand plus a stale one")
	}
	h.askResult = true
	c := &Ctx{Source: src.ID, Controller: 0,
		Remembered: []state.Target{{Obj: first.ID}, {Obj: extraA.ID}, {Obj: second.ID}, {Obj: extraB.ID}, {Obj: stale.ID}}}
	Resolve(h, c, body)
	if h.lastAsk == nil || h.lastAsk.ResumeKind != "hand_move" || len(h.lastAsk.Options) != 2 {
		t.Fatalf("owner A not offered its two remembered candidates: %+v", h.lastAsk)
	}
	// Owner A answers on the same walk state (the first re-entry, whose Ctx
	// still holds the pre-clear set); owner B's ask is posed on that pass.
	c.HandMove, c.HandMoveDone, c.HandMoveTarget = []state.ObjID{first.ID}, true, 0
	Resolve(h, c, body)
	if first.Zone != state.ZExile {
		t.Fatalf("owner A's answer not settled: zone %s", first.Zone)
	}
	if h.lastAsk == nil || h.lastAsk.ResumeTarget != 1 || len(h.lastAsk.Options) != 2 ||
		h.lastAsk.Options[0].Obj != second.ID || h.lastAsk.Options[1].Obj != extraB.ID {
		t.Fatalf("owner B's remembered candidates lost after A's settle: %+v", h.lastAsk)
	}
	// A real host rebuilds a FRESH Ctx from the answered decision; exercise
	// that boundary for owner B's re-entry.
	r := forgetRide{d: h.lastAsk}
	c = r.ctx(&Ctx{Source: src.ID, Controller: 0})
	c.HandMove, c.HandMoveDone, c.HandMoveTarget = []state.ObjID{second.ID}, true, 1
	h.askResult = false
	Resolve(h, c, body)
	if c.ForgetOtherReady || c.ForgetOtherCleared {
		t.Fatal("multi-owner snapshot leaked into the next ability")
	}
	if first.Zone != state.ZExile || second.Zone != state.ZExile {
		t.Fatalf("answered cards not both exiled: first=%s second=%s", first.Zone, second.Zone)
	}
	if extraA.Zone != state.ZHand || extraB.Zone != state.ZHand {
		t.Fatalf("unanswered hand cards disturbed: extraA=%s extraB=%s", extraA.Zone, extraB.Zone)
	}
	if got := rememberedIDs(h.g.Obj(src.ID).Remembered); !sameIDs(got, []state.ObjID{first.ID, second.ID}) {
		t.Errorf("persistent remembered %v, want exactly the two moved cards", got)
	}
	if n := clearRememberedCount(h, src.ID); n != 1 {
		t.Errorf("clear-remembered emitted %d times across two owners and a suspension, want 1", n)
	}
}

// The whole-hand shape (effChangeZoneHand): one owner, one ask, and the
// answered re-entry runs in a FRESH Ctx rebuilt from the ask's ride. The
// pre-clear selector must still admit the answered card at revalidation,
// the clear must fire exactly once, and the re-remembered result survives.
func TestHandMoveForgetOtherRememberedHandAsk(t *testing.T) {
	h, src, cards := forgetFixtureHost(t, "Hand Pick", "Other Hand Pick", "Stale")
	picked, other, stale := cards[0], cards[1], cards[2]
	h.g.SetZone(state.ZHand, 0, []state.ObjID{picked.ID, other.ID})
	picked.Zone, other.Zone = state.ZHand, state.ZHand
	h.g.SetZone(state.ZExile, 0, []state.ObjID{stale.ID})
	stale.Zone = state.ZExile
	seedRemembered(h, src, picked.ID, other.ID, stale.ID)
	body := sa(t, "DB$ ChangeZone | Origin$ Hand | Destination$ Exile | ChangeType$ Card.IsRemembered | ChangeNum$ 1 | Mandatory$ True | RememberChanged$ True | ForgetOtherRemembered$ True")
	if picked.ID == other.ID || picked.Zone != state.ZHand ||
		!sameIDs(rememberedIDs(h.g.Obj(src.ID).Remembered), []state.ObjID{picked.ID, other.ID, stale.ID}) {
		t.Fatal("precondition: two remembered cards in hand plus a stale one")
	}
	h.askResult = true
	c := &Ctx{Source: src.ID, Controller: 0,
		Remembered: []state.Target{{Obj: picked.ID}, {Obj: other.ID}, {Obj: stale.ID}}}
	Resolve(h, c, body)
	if h.lastAsk == nil || h.lastAsk.ResumeKind != "hand_move" || len(h.lastAsk.Options) != 2 {
		t.Fatalf("hand ask missing the remembered options: %+v", h.lastAsk)
	}
	r := forgetRide{d: h.lastAsk}
	c = r.ctx(&Ctx{Source: src.ID, Controller: 0})
	c.HandMove, c.HandMoveDone, c.HandMoveTarget = []state.ObjID{picked.ID}, true, 0
	h.askResult = false
	Resolve(h, c, body)
	if c.ForgetOtherReady || c.ForgetOtherCleared {
		t.Fatal("snapshot leaked into the next ability")
	}
	if picked.Zone != state.ZExile || other.Zone != state.ZHand {
		t.Fatalf("zones picked=%s other=%s", picked.Zone, other.Zone)
	}
	if got := rememberedIDs(h.g.Obj(src.ID).Remembered); !sameIDs(got, []state.ObjID{picked.ID}) {
		t.Errorf("persistent remembered %v, want the moved card only", got)
	}
	if n := clearRememberedCount(h, src.ID); n != 1 {
		t.Errorf("clear-remembered emitted %d times across a suspension, want 1", n)
	}
}

// Dig: player 0's answered take clears the set on pass 1 (the clear sits
// BEFORE the ask); player 1's take ask is posed only on the resumed pass,
// so its eligibility filter reads the pre-clear candidates through the
// ask's ride or sees nothing.
func TestDigForgetOtherRememberedAsk(t *testing.T) {
	h, src, cards := forgetFixtureHost(t, "First Dug", "Second Dug", "Third Dug", "Stale")
	first, third, stale := cards[0], cards[2], cards[3]
	second := h.g.Obj(h.g.AddObject(mkCard(t, "Name:Second Dug\nTypes:Creature\nPT:3/3\nOracle:x\n"), 1).ID)
	first, third, stale = h.g.Obj(first.ID), h.g.Obj(third.ID), h.g.Obj(stale.ID)
	cards[1] = h.g.Obj(cards[1].ID)
	h.g.SetZone(state.ZLibrary, 0, []state.ObjID{first.ID, cards[1].ID})
	first.Zone, cards[1].Zone = state.ZLibrary, state.ZLibrary
	h.g.SetZone(state.ZLibrary, 1, []state.ObjID{second.ID, third.ID})
	second.Zone, third.Zone = state.ZLibrary, state.ZLibrary
	h.g.SetZone(state.ZExile, 0, []state.ObjID{stale.ID})
	stale.Zone = state.ZExile
	seedRemembered(h, src, first.ID, cards[1].ID, second.ID, third.ID, stale.ID)
	body := sa(t, "DB$ Dig | DigNum$ 2 | ChangeNum$ 1 | ChangeValid$ Creature.IsRemembered | DestinationZone$ Exile | Defined$ You & Opponent | RememberChanged$ True | ForgetOtherRemembered$ True")
	if body.API != "Dig" {
		t.Fatalf("precondition: fixture SA parsed as %q, want Dig", body.API)
	}
	if first.Zone != state.ZLibrary || second.Zone != state.ZLibrary ||
		!sameIDs(rememberedIDs(h.g.Obj(src.ID).Remembered), []state.ObjID{first.ID, cards[1].ID, second.ID, third.ID, stale.ID}) {
		t.Fatal("precondition: two remembered creatures on top of each library")
	}
	h.askResult = true
	c := &Ctx{Source: src.ID, Controller: 0,
		Remembered: []state.Target{{Obj: first.ID}, {Obj: cards[1].ID}, {Obj: second.ID}, {Obj: third.ID}, {Obj: stale.ID}}}
	Resolve(h, c, body)
	if h.lastAsk == nil || h.lastAsk.ResumeKind != "dig" || h.lastAsk.ResumeTarget != 0 || len(h.lastAsk.Options) != 2 {
		t.Fatalf("player 0 not offered its two remembered window cards: %+v", h.lastAsk)
	}
	r := forgetRide{d: h.lastAsk}
	c = r.ctx(&Ctx{Source: src.ID, Controller: 0})
	c.Dig, c.DigDone, c.DigTarget = []state.ObjID{first.ID}, true, 0
	Resolve(h, c, body)
	if h.lastAsk == nil || h.lastAsk.ResumeKind != "dig" || h.lastAsk.ResumeTarget != 1 ||
		len(h.lastAsk.Options) != 2 || h.lastAsk.Options[0].Obj != second.ID {
		t.Fatalf("player 1's remembered window lost after player 0's answer: %+v", h.lastAsk)
	}
	r = forgetRide{d: h.lastAsk}
	c = r.ctx(&Ctx{Source: src.ID, Controller: 0})
	c.Dig, c.DigDone, c.DigTarget = []state.ObjID{second.ID}, true, 1
	h.askResult = false
	Resolve(h, c, body)
	if c.ForgetOtherReady || c.ForgetOtherCleared {
		t.Fatal("snapshot leaked into the next ability")
	}
	if first.Zone != state.ZExile || second.Zone != state.ZExile {
		t.Fatalf("answered takes not both exiled: first=%s second=%s", first.Zone, second.Zone)
	}
	if cards[1].Zone != state.ZLibrary || third.Zone != state.ZLibrary {
		t.Fatalf("untaken window cards disturbed: cards[1]=%s third=%s", cards[1].Zone, third.Zone)
	}
	if n := clearRememberedCount(h, src.ID); n != 1 {
		t.Errorf("clear-remembered emitted %d times across two windows and suspensions, want 1", n)
	}
}

// DigUntil: the found-move election suspends the walk; the answered
// re-entry RE-RUNS the Valid$ scan for the answered library, so the
// pre-clear candidates must ride the ask or the found card is never moved.
func TestDigUntilForgetOtherRememberedAsk(t *testing.T) {
	h, src, cards := forgetFixtureHost(t, "Found Creature", "Stale")
	found, stale := cards[0], cards[1]
	filler := h.g.Obj(h.g.AddObject(mkCard(t, "Name:Filler\nTypes:Basic Land Island\nOracle:x\n"), 0).ID)
	found, stale = h.g.Obj(found.ID), h.g.Obj(stale.ID)
	h.g.SetZone(state.ZLibrary, 0, []state.ObjID{found.ID, filler.ID})
	found.Zone, filler.Zone = state.ZLibrary, state.ZLibrary
	h.g.SetZone(state.ZExile, 0, []state.ObjID{stale.ID})
	stale.Zone = state.ZExile
	seedRemembered(h, src, found.ID, stale.ID)
	body := sa(t, "DB$ DigUntil | Valid$ Creature.IsRemembered | OptionalFoundMove$ True | RememberFound$ True | ForgetOtherRemembered$ True")
	if body.API != "DigUntil" {
		t.Fatalf("precondition: fixture SA parsed as %q, want DigUntil", body.API)
	}
	if found.Zone != state.ZLibrary || stale.ID == found.ID ||
		!sameIDs(rememberedIDs(h.g.Obj(src.ID).Remembered), []state.ObjID{found.ID, stale.ID}) {
		t.Fatal("precondition: remembered library candidate and stale exile candidate")
	}
	h.askResult = true
	c := &Ctx{Source: src.ID, Controller: 0, Remembered: []state.Target{{Obj: found.ID}, {Obj: stale.ID}}}
	Resolve(h, c, body)
	if h.lastAsk == nil || h.lastAsk.ResumeKind != "diguntil_move" {
		t.Fatalf("found-move election not posed: %+v", h.lastAsk)
	}
	r := forgetRide{d: h.lastAsk}
	c = r.ctx(&Ctx{Source: src.ID, Controller: 0})
	c.DigUntilMove, c.DigUntilMoveDone = "yes", true
	h.askResult = false
	Resolve(h, c, body)
	if c.ForgetOtherReady || c.ForgetOtherCleared {
		t.Fatal("snapshot leaked into the next ability")
	}
	if found.Zone != state.ZHand {
		t.Fatalf("accepted found card not moved to hand: zone %s", found.Zone)
	}
	if filler.Zone != state.ZLibrary {
		t.Errorf("the filler card was disturbed: zone %s", filler.Zone)
	}
	if got := rememberedIDs(c.Remembered); !sameIDs(got, []state.ObjID{found.ID}) {
		t.Errorf("remembered after the found move %v, want the found card", got)
	}
	if n := clearRememberedCount(h, src.ID); n != 1 {
		t.Errorf("clear-remembered emitted %d times across a suspension, want 1", n)
	}
}

// ChooseCard: chooser 0's answered choice clears the set; chooser 1's pool
// (cardChoices re-runs on every resumed pass) must still match the
// pre-clear candidates through the ask's ride.
func TestChooseCardForgetOtherRememberedAsk(t *testing.T) {
	h, src, cards := forgetFixtureHost(t, "First Kept", "Second Kept", "Stale")
	first, second, stale := cards[0], cards[1], cards[2]
	filler := h.g.Obj(h.g.AddObject(mkCard(t, "Name:Grave Filler\nTypes:Basic Land Island\nOracle:x\n"), 0).ID)
	first, second, stale = h.g.Obj(first.ID), h.g.Obj(second.ID), h.g.Obj(stale.ID)
	h.g.SetZone(state.ZGraveyard, 0, []state.ObjID{first.ID, filler.ID})
	first.Zone, filler.Zone = state.ZGraveyard, state.ZGraveyard
	h.g.SetZone(state.ZGraveyard, 1, []state.ObjID{second.ID})
	second.Zone = state.ZGraveyard
	h.g.SetZone(state.ZExile, 0, []state.ObjID{stale.ID})
	stale.Zone = state.ZExile
	seedRemembered(h, src, first.ID, second.ID, stale.ID)
	body := sa(t, "DB$ ChooseCard | Defined$ You & Opponent | Amount$ 1 | Choices$ Creature.IsRemembered | ChoiceZone$ Graveyard | Mandatory$ True | RememberChosen$ True | ForgetOtherRemembered$ True")
	if body.API != "ChooseCard" {
		t.Fatalf("precondition: fixture SA parsed as %q, want ChooseCard", body.API)
	}
	if first.Zone != state.ZGraveyard || second.Zone != state.ZGraveyard ||
		!sameIDs(rememberedIDs(h.g.Obj(src.ID).Remembered), []state.ObjID{first.ID, second.ID, stale.ID}) {
		t.Fatal("precondition: one remembered creature per chooser's graveyard")
	}
	h.askResult = true
	c := &Ctx{Source: src.ID, Controller: 0,
		Remembered: []state.Target{{Obj: first.ID}, {Obj: second.ID}, {Obj: stale.ID}}}
	Resolve(h, c, body)
	if h.lastAsk == nil || h.lastAsk.ResumeKind != "choice" || h.lastAsk.ResumeTarget != 0 ||
		len(h.lastAsk.Options) == 0 || h.lastAsk.Options[0].Obj != first.ID {
		t.Fatalf("chooser 0 not offered its remembered creature: %+v", h.lastAsk)
	}
	r := forgetRide{d: h.lastAsk}
	c = r.ctx(&Ctx{Source: src.ID, Controller: 0})
	c.Choice, c.ChoiceDone, c.ChoiceTarget = []state.Target{{Obj: first.ID}}, true, 0
	Resolve(h, c, body)
	if h.lastAsk == nil || h.lastAsk.ResumeTarget != 1 || len(h.lastAsk.Options) == 0 ||
		h.lastAsk.Options[len(h.lastAsk.Options)-1].Obj != second.ID {
		t.Fatalf("chooser 1's remembered pool lost after chooser 0's answer: %+v", h.lastAsk)
	}
	r = forgetRide{d: h.lastAsk}
	c = r.ctx(&Ctx{Source: src.ID, Controller: 0})
	c.Choice, c.ChoiceDone, c.ChoiceTarget = []state.Target{{Obj: second.ID}}, true, 1
	h.askResult = false
	Resolve(h, c, body)
	if c.ForgetOtherReady || c.ForgetOtherCleared {
		t.Fatal("snapshot leaked into the next ability")
	}
	if got := rememberedIDs(c.Chosen); !sameIDs(got, []state.ObjID{first.ID, second.ID}) {
		t.Fatalf("chosen cards %v, want both choosers' picks", got)
	}
	if got := rememberedIDs(c.Remembered); !sameIDs(got, []state.ObjID{first.ID, second.ID}) {
		t.Errorf("remembered after both choices %v, want both picks", got)
	}
	if n := clearRememberedCount(h, src.ID); n != 1 {
		t.Errorf("clear-remembered emitted %d times across two choosers and a suspension, want 1", n)
	}
}
