package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// The ExiledWith$Colors ref property (ticket levelb-static-count-attachments):
// Sunbird Effigy's characteristic-defining P/T ("power and toughness are each
// equal to the number of colors among the exiled cards used to craft it")
// counts DISTINCT colours among the cards the source exiled, the same
// distinct-set read Count$Valid <spec>$Colors makes over zone matches. Before
// the property existed the body read zero and every Craft carrier's Effigy
// printed 0/0 and died. The test loads the recorded body off the real corpus
// card, the cast_ref_head_test.go pattern.
func TestExiledWithColorsCountsDistinctColours(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	effigy, ok := reg.Lookup("Sunbird Standard")
	if !ok || len(effigy.Faces) < 2 {
		t.Fatal("corpus missing Sunbird Standard")
	}
	body := effigy.Faces[1].SVars["X"]
	// The recorded body is the point of the pin: if the corpus spelling ever
	// changes, this test must move with it, not silently pass.
	if body != "ExiledWith$Colors" {
		t.Fatalf("Sunbird Effigy SVar X = %q, want ExiledWith$Colors", body)
	}

	h, c := fixtureHost(t)
	src := c.Source
	green := mkCard(t, "Name:Green Material\nManaCost:1 G\nTypes:Creature\nPT:1/1\nColors:green\nOracle:x\n")
	gold := mkCard(t, "Name:Gold Material\nManaCost:W R\nTypes:Creature\nPT:2/2\nColors:white, red\nOracle:x\n")
	colorless := mkCard(t, "Name:Grey Material\nManaCost:3\nTypes:Artifact\nOracle:x\n")
	foreign := mkCard(t, "Name:Foreign Exile\nManaCost:2 B\nTypes:Creature\nPT:2/2\nColors:black\nOracle:x\n")
	greenObj := h.g.AddObject(green, 0)
	goldObj := h.g.AddObject(gold, 0)
	colorlessObj := h.g.AddObject(colorless, 0)
	foreignObj := h.g.AddObject(foreign, 0)
	// The craft material association: each exiled card's ExiledWith names the
	// exiling source. The foreign card sits in the same zone but was exiled
	// by something else, so it must not count.
	for _, id := range []state.ObjID{greenObj.ID, goldObj.ID, colorlessObj.ID} {
		h.g.SetZone(state.ZExile, 0, append(h.g.Zone(state.ZExile, 0), id))
		h.g.Obj(id).Zone = state.ZExile
		h.g.Obj(id).ExiledWith = src
	}
	h.g.SetZone(state.ZExile, 0, append(h.g.Zone(state.ZExile, 0), foreignObj.ID))
	h.g.Obj(foreignObj.ID).Zone = state.ZExile
	h.g.Obj(foreignObj.ID).ExiledWith = src + 1

	// Precondition: the association read is the one the count walks, and the
	// three owned exiles plus the foreign one are all in the zone.
	if len(h.g.Zone(state.ZExile, 0)) != 4 {
		t.Fatalf("precondition: %d cards in exile, want 4", len(h.g.Zone(state.ZExile, 0)))
	}

	// Green contributes one colour, gold two distinct ones, the colorless
	// material none: the distinct count is three, not four cards and not the
	// five colour occurrences.
	n, ok := EvalCountOK(h, c, body)
	if !ok {
		t.Fatalf("ExiledWith$Colors unresolvable")
	}
	if n != 3 {
		t.Fatalf("ExiledWith$Colors = %d, want 3 (green + white/red, the colorless material and the foreign exile excluded)", n)
	}

	// An empty association reads a legitimate zero, not unresolvable: the
	// Effigy's printed 0/0 baseline.
	h.g.SetZone(state.ZExile, 0, nil)
	for _, id := range []state.ObjID{greenObj.ID, goldObj.ID, colorlessObj.ID, foreignObj.ID} {
		h.g.Obj(id).ExiledWith = 0
	}
	if n, ok := EvalCountOK(h, c, body); !ok || n != 0 {
		t.Fatalf("empty ExiledWith$Colors = (%d, %v), want (0, true)", n, ok)
	}
}
