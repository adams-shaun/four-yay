package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

// TestCompoundLibraryAndHandOriginUsesOneUnionChooser covers the mixed hidden
// origin that previously emitted the loud fallback note. Both candidates are
// in the controller's named origins, so a source-default object path would be
// observably wrong.
func TestCompoundLibraryAndHandOriginUsesOneUnionChooser(t *testing.T) {
	h := &askHost{}
	h.g = state.NewGame(names(2))
	source := h.g.AddObject(mkCard(t, "Name:Source\nTypes:Sorcery\nOracle:x\n"), 0)
	c := &Ctx{Source: source.ID, Controller: 0}
	libraryCard := h.g.AddObject(mkCard(t, "Name:Library Creature\nTypes:Creature\nPT:2/2\nOracle:x\n"), 0)
	libraryCard.Zone = state.ZLibrary
	handCard := h.g.AddObject(mkCard(t, "Name:Hand Creature\nTypes:Creature\nPT:3/3\nOracle:x\n"), 0)
	handCard.Zone = state.ZHand
	h.g.SetZone(state.ZLibrary, 0, []state.ObjID{libraryCard.ID})
	h.g.SetZone(state.ZHand, 0, []state.ObjID{handCard.ID})
	if libraryCard.Zone == handCard.Zone || libraryCard.ID == handCard.ID {
		t.Fatal("test setup did not create distinct library and hand candidates")
	}

	s := sa(t, "DB$ ChangeZone | Origin$ Library,Hand | Destination$ Battlefield | ChangeType$ Creature")
	h.asked = nil
	effChangeZone(h, c, s)
	if h.asked == nil {
		t.Fatal("mixed library/hand origin did not pose a chooser")
	}
	if h.asked.ResumeKind != "search" || len(h.asked.Options) != 2 {
		t.Fatalf("decision = %+v, want one search decision with two options", h.asked)
	}
	for _, id := range []state.ObjID{libraryCard.ID, handCard.ID} {
		found := false
		for _, option := range h.asked.Options {
			if option.Obj == id {
				found = true
			}
		}
		if !found {
			t.Fatalf("candidate %d absent from union options: %+v", id, h.asked.Options)
		}
	}
}

// TestCompoundHandAndGraveyardOriginUsesOneUnionChooser covers the
// non-library mixed-hand form: it must not fall through to the source-default
// object path merely because Library is absent.
func TestCompoundHandAndGraveyardOriginUsesOneUnionChooser(t *testing.T) {
	h := &askHost{}
	h.g = state.NewGame(names(2))
	source := h.g.AddObject(mkCard(t, "Name:Source\nTypes:Sorcery\nOracle:x\n"), 0)
	c := &Ctx{Source: source.ID, Controller: 0}
	handCard := h.g.AddObject(mkCard(t, "Name:Hand Creature\nTypes:Creature\nPT:3/3\nOracle:x\n"), 0)
	handCard.Zone = state.ZHand
	graveyardCard := h.g.AddObject(mkCard(t, "Name:Graveyard Creature\nTypes:Creature\nPT:4/4\nOracle:x\n"), 0)
	graveyardCard.Zone = state.ZGraveyard
	h.g.SetZone(state.ZHand, 0, []state.ObjID{handCard.ID})
	h.g.SetZone(state.ZGraveyard, 0, []state.ObjID{graveyardCard.ID})
	if handCard.Zone == graveyardCard.Zone || handCard.ID == graveyardCard.ID {
		t.Fatal("test setup did not create distinct hand and graveyard candidates")
	}

	effChangeZone(h, c, sa(t, "DB$ ChangeZone | Origin$ Hand,Graveyard | Destination$ Battlefield | ChangeType$ Creature"))
	if h.asked == nil || h.asked.ResumeKind != "search" || len(h.asked.Options) != 2 {
		t.Fatalf("decision = %+v, want one search decision with two options", h.asked)
	}
	for _, id := range []state.ObjID{handCard.ID, graveyardCard.ID} {
		found := false
		for _, option := range h.asked.Options {
			if option.Obj == id {
				found = true
			}
		}
		if !found {
			t.Fatalf("candidate %d absent from union options: %+v", id, h.asked.Options)
		}
	}
}
