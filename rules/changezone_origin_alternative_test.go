package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// TestChangeZoneOriginAlternativeSearchesEveryNamedZone is the
// OriginAlternative$ concern: `Origin$ Library | OriginAlternative$
// Graveyard,Hand` ("search your graveyard, hand, and/or library") must offer
// ONE candidate set spanning every named zone, not a library-only search.
// Gate to the Afterlife is the corpus carrier; the pinned case puts
// God-Pharaoh's Gift in the GRAVEYARD (not the library) and proves the search
// offers it and moves it onto the battlefield.
func TestChangeZoneOriginAlternativeSearchesEveryNamedZone(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := searchEngine(t, reg, "Gate to the Afterlife", "God-Pharaoh's Gift", "God-Pharaoh's Gift")
	gate := searchMoveByName(t, e, "Gate to the Afterlife", state.ZBattlefield)
	// The activation gate needs six creature cards in the graveyard
	// (CheckSVar$ Count$ValidGraveyard Creature.YouOwn | SVarCompare$ GE6).
	// Grizzly Bears is the fixture's Creature filler.
	for i := 0; i < 6; i++ {
		searchMoveByName(t, e, "Grizzly Bears", state.ZGraveyard)
	}
	// The named card sits in the graveyard, nowhere near the library; a second
	// copy sits in HAND, so the same decision proves BOTH alternate zones of
	// `OriginAlternative$ Graveyard,Hand` join the library's option list.
	gift := searchMoveByName(t, e, "God-Pharaoh's Gift", state.ZGraveyard)
	giftHand := searchMoveByName(t, e, "God-Pharaoh's Gift", state.ZHand)
	e.pending = nil
	e.priorityRound()

	// Gate entered this turn, so wait out summoning sickness before the tap
	// cost can be paid (CR 302.6).
	driveToStep(t, e, 3, 0, state.StepMain1)
	addMana(t, e, 0, "CC")
	idx := changeZoneAbilityIndexBy(t, e, gate, "Library")
	submitChoices(t, e, abilityOption(t, e, gate, idx).Index)
	// The CARDNAME sacrifice cost is a real KChoose over one option.
	if d := e.Pending(); d != nil && d.Kind == decision.KChoose && d.ResumeKind != "search" {
		if len(d.Options) != 1 || d.Options[0].Obj != gate {
			t.Fatalf("unexpected activation cost choice: %+v", d)
		}
		submitChoices(t, e, d.Options[0].Index)
	}
	d := passUntilNonPriority(t, e, 20)
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "search" {
		t.Fatalf("origin-alternative search pending = %+v, want a search KChoose", d)
	}
	// The graveyard-resident God-Pharaoh's Gift must be among the options, and
	// so must the hand-resident copy: both named alternate origins are merged.
	var pick decision.Option
	found, foundHand := false, false
	for _, o := range d.Options {
		if o.Obj == gift {
			pick, found = o, true
		}
		if o.Obj == giftHand {
			foundHand = true
		}
	}
	if !found {
		t.Fatalf("God-Pharaoh's Gift in the graveyard was not offered: %+v", d.Options)
	}
	if !foundHand {
		t.Fatalf("God-Pharaoh's Gift in hand was not offered: %+v", d.Options)
	}
	submitChoices(t, e, pick.Index)
	passUntilStackEmpty(t, e, 30)

	if o := e.G.Obj(gift); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("God-Pharaoh's Gift zone = %v, want battlefield", o)
	}
	if o := e.G.Obj(gift); o != nil && o.Controller != 0 {
		t.Fatalf("God-Pharaoh's Gift controller = %d, want the searcher", o.Controller)
	}
	replayCheck(t, e, cfg)
}
