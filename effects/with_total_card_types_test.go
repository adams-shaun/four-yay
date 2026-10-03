package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func TestWithTotalCardTypesRejectsInsufficientHiddenPick(t *testing.T) {
	g := state.NewGame(names(2))
	creature := mkCard(t, "Name:A\nTypes:Creature\nPT:2/2\nOracle:x\n")
	otherCreature := mkCard(t, "Name:B\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	a := g.AddObject(creature, 0)
	b := g.AddObject(otherCreature, 0)
	g.SetZone(state.ZGraveyard, 0, []state.ObjID{a.ID, b.ID})
	a.Zone, b.Zone = state.ZGraveyard, state.ZGraveyard
	h := &fakeHost{g: g}
	c := &Ctx{Controller: 0, Source: a.ID}
	ability := sa(t, "DB$ ChangeZone | Origin$ Graveyard | Destination$ Exile | ChangeNum$ 2 | ChangeType$ Card | WithTotalCardTypes$ 2")

	// Both candidates are creatures, so the chosen set has only one distinct
	// card type and must fail closed rather than exiling an invalid set.
	applyLibrarySearch(h, c, ability, ChangeZoneOf(ability), 0, state.ZExile, []state.ObjID{a.ID, b.ID}, []state.Zone{state.ZGraveyard})
	if a.Zone != state.ZGraveyard || b.Zone != state.ZGraveyard {
		t.Fatalf("insufficient-type pick moved cards: zones = %s, %s", a.Zone, b.Zone)
	}
	foundNote := false
	for _, ev := range h.log {
		if ev.Kind == events.Note && ev.Text == "hidden pick fails WithTotalCardTypes$ requirement" {
			foundNote = true
		}
	}
	if !foundNote {
		t.Fatalf("failed hidden pick did not record a Note witness: log=%+v params=%+v", h.log, ability.Params)
	}
}
