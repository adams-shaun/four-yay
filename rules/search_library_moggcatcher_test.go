package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func TestMoggcatcherSearchFindsGoblinPermanentCard(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := searchEngine(t, reg, "Moggcatcher", "Goblin Piker")
	catcher := searchMoveByName(t, e, "Moggcatcher", state.ZBattlefield)
	piker := searchMoveByName(t, e, "Goblin Piker", state.ZLibrary)
	if e.G.Obj(piker).Zone != state.ZLibrary {
		t.Fatalf("Goblin Piker zone = %s, want library", e.G.Obj(piker).Zone)
	}
	// The fixture moves the source in after the opening turn; advance once so
	// Moggcatcher is no longer summoning-sick and its tap ability is offered.
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: e.G.Turn + 1})
	e.priorityRound()
	if effects.MatchesSpecFrom(e.G, "Permanent", piker, 0, 0) {
		t.Fatal("bare Permanent unexpectedly matches a library card")
	}
	if !effects.MatchesSpecFrom(e.G, "PermanentCard.Goblin", piker, 0, 0) {
		t.Fatal("Goblin Piker is not a permanent Goblin card")
	}

	face := e.G.Obj(catcher).Face()
	idx := changeZoneAbilityIndex(t, e, catcher)
	sa := face.Abilities[idx]
	if sa.Params["Origin"] != "Library" || sa.Params["ChangeType"] != "Permanent.Goblin" {
		t.Fatalf("Moggcatcher ability = Origin %q ChangeType %q, want Library/Permanent.Goblin", sa.Params["Origin"], sa.Params["ChangeType"])
	}

	addMana(t, e, 0, "CCC")
	d := activateSearch(t, e, catcher)
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "search" {
		t.Fatalf("pending = %+v, want a search KChoose", d)
	}
	if d.Min != 0 || d.Max != 1 {
		t.Fatalf("Min/Max = %d/%d, want 0/1", d.Min, d.Max)
	}
	found := false
	for _, option := range d.Options {
		if option.Obj == piker {
			found = true
		}
	}
	if !found {
		t.Fatalf("search options = %+v, want Goblin Piker (%d)", d.Options, piker)
	}

	submitChoices(t, e, func() int {
		for _, option := range d.Options {
			if option.Obj == piker {
				return option.Index
			}
		}
		return -1
	}())
	if e.G.Obj(piker).Zone != state.ZBattlefield {
		t.Fatalf("Goblin Piker zone = %s, want battlefield", e.G.Obj(piker).Zone)
	}
	replayCheck(t, e, cfg)
}
