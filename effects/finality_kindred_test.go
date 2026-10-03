package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

func TestWithTotalCardTypesCountsKindredAsCardType(t *testing.T) {
	g := state.NewGame(names(2))
	cards := []*cards.Card{
		mkCard(t, "Name:Kindred Relic\nTypes:Kindred\nOracle:x\n"),
		mkCard(t, "Name:Bear\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"),
		mkCard(t, "Name:Land\nTypes:Land\nOracle:x\n"),
		mkCard(t, "Name:Enchant\nTypes:Enchantment\nOracle:x\n"),
	}
	ids := make([]state.ObjID, len(cards))
	for i, card := range cards {
		o := g.AddObject(card, 0)
		o.Zone = state.ZGraveyard
		ids[i] = o.ID
	}
	g.SetZone(state.ZGraveyard, 0, ids)
	h := &fakeHost{g: g}
	if !totalCardTypesSatisfied(g, ids, 4) {
		t.Fatal("precondition: Kindred must count as a distinct card type")
	}
	finality := sa(t, "DB$ ChangeZone | Origin$ Graveyard | Destination$ Exile | ChangeNum$ 4 | ChangeType$ Card | WithTotalCardTypes$ 4")
	applyLibrarySearch(h, &Ctx{Controller: 0, Source: ids[0]}, finality, ChangeZoneOf(finality), 0, state.ZExile, ids, []state.Zone{state.ZGraveyard})
	for _, id := range ids {
		if got := g.Obj(id).Zone; got != state.ZExile {
			t.Fatalf("four-type pick with Kindred left %d in %s", id, got)
		}
	}
}
