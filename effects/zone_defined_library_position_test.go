package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func TestDefinedLibraryPositionFetchesDoNotShuffle(t *testing.T) {
	for _, tc := range []struct {
		name, defined string
		position      int
	}{
		{name: "top", defined: "TopOfLibrary", position: 0},
		{name: "bottom", defined: "BottomOfLibrary", position: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHost(t, 2)
			source := h.g.AddObject(mkCard(t, "Name:Source\nTypes:Sorcery\nOracle:x\n"), 0)
			cardsInLibrary := []state.ObjID{
				h.g.AddObject(mkCard(t, "Name:First\nTypes:Creature\nPT:2/2\nOracle:x\n"), 0).ID,
				h.g.AddObject(mkCard(t, "Name:Middle\nTypes:Creature\nPT:2/2\nOracle:x\n"), 0).ID,
				h.g.AddObject(mkCard(t, "Name:Last\nTypes:Creature\nPT:2/2\nOracle:x\n"), 0).ID,
			}
			h.g.SetZone(state.ZLibrary, 0, cardsInLibrary)
			for _, id := range cardsInLibrary {
				h.g.Obj(id).Zone = state.ZLibrary
			}
			selected := cardsInLibrary[tc.position]
			if got := h.g.Zone(state.ZLibrary, 0); len(got) != 3 || got[tc.position] != selected {
				t.Fatalf("precondition: library order %v does not place selected %d at %s position", got, selected, tc.name)
			}

			Resolve(h, &Ctx{Controller: 0, Source: source.ID}, sa(t,
				"DB$ ChangeZone | Origin$ Library | Destination$ Graveyard | Defined$ "+tc.defined))

			if got := h.g.Obj(selected).Zone; got != state.ZGraveyard {
				t.Fatalf("selected library card zone = %s, want graveyard", got)
			}
			shuffles := 0
			for _, event := range h.log {
				if event.Kind == events.Shuffle && event.Player == 0 {
					shuffles++
				}
			}
			if shuffles != 0 {
				t.Fatalf("%s position fetch emitted %d Shuffle events, want zero: %+v", tc.name, shuffles, h.log)
			}
		})
	}

	t.Run("remembered still shuffles", func(t *testing.T) {
		h := newHost(t, 2)
		source := h.g.AddObject(mkCard(t, "Name:Source\nTypes:Sorcery\nOracle:x\n"), 0)
		fetched := h.g.AddObject(mkCard(t, "Name:Remembered\nTypes:Creature\nPT:2/2\nOracle:x\n"), 0)
		other := h.g.AddObject(mkCard(t, "Name:Other\nTypes:Creature\nPT:2/2\nOracle:x\n"), 0)
		library := []state.ObjID{fetched.ID, other.ID}
		h.g.SetZone(state.ZLibrary, 0, library)
		for _, id := range library {
			h.g.Obj(id).Zone = state.ZLibrary
		}
		if len(library) != 2 || library[0] != fetched.ID || library[0] == library[1] {
			t.Fatalf("precondition: remembered card %d is distinct and in the library: %v", fetched.ID, library)
		}

		Resolve(h, &Ctx{Controller: 0, Source: source.ID, Remembered: []state.Target{{Obj: fetched.ID}}}, sa(t,
			"DB$ ChangeZone | Origin$ Library | Destination$ Graveyard | Defined$ Remembered"))

		if got := h.g.Obj(fetched.ID).Zone; got != state.ZGraveyard {
			t.Fatalf("remembered card zone = %s, want graveyard", got)
		}
		shuffles := 0
		for _, event := range h.log {
			if event.Kind == events.Shuffle && event.Player == 0 {
				shuffles++
			}
		}
		if shuffles != 1 {
			t.Fatalf("Defined$ Remembered emitted %d Shuffle events, want one: %+v", shuffles, h.log)
		}
	})
}
