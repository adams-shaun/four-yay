package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// This file pins DigUntil's MinTotalCMC$ threshold against the REAL compiled
// Dream Harvest SA (ECL), not a synthetic script. The fix that reads the
// threshold is 73c9cc2e5 ("consume six unread std3 effect params at their
// readers"); before it effDigUntil fell through to the ordinary Amount$ scan
// (amount defaults to 1) and stopped after the first matching card.
//
// The parent test (TestDigUntilMinTotalCMCRevealsPastTheThreshold in
// unread_param_fixes_test.go) uses an inline script and a zero-mana-value
// library. These two close the remaining gap: the corpus card itself, and a
// library whose cumulative sum is non-zero so "sum >= threshold" is a real
// assertion rather than a vacuous one.

// dreamHarvestRoot loads Dream Harvest from the corpus and returns the root
// DigUntil SA, failing loudly if the corpus pin moved under the test.
func dreamHarvestRoot(t *testing.T) (*cards.Registry, *cards.SA) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	card, ok := reg.Lookup("Dream Harvest")
	if !ok {
		t.Fatal("corpus pin moved: Dream Harvest is missing")
	}
	if len(card.Faces) == 0 || len(card.Faces[0].Abilities) == 0 {
		t.Fatal("corpus pin moved: Dream Harvest has no root ability")
	}
	root := card.Faces[0].Abilities[0]
	if root == nil || root.API != "DigUntil" {
		if root == nil {
			t.Fatal("corpus pin moved: Dream Harvest root ability is nil")
		}
		t.Fatalf("corpus pin moved: root API = %q, want DigUntil", root.API)
	}
	return reg, root
}

// zeroMVManaScript is a card with no ManaCost: its mana value is 0, so a
// library of them can never reach any positive threshold and must exile whole.
const zeroMVManaScript = "Name:Filler\nTypes:Creature\nPT:1/1\nOracle:x\n"

func seededLibrary(t *testing.T, h *fakeHost, p state.PlayerID, cards ...*cards.Card) []state.ObjID {
	t.Helper()
	var ids []state.ObjID
	for _, c := range cards {
		ids = append(ids, h.g.AddObject(c, p).ID)
	}
	h.g.SetZone(state.ZLibrary, p, ids)
	if got := h.g.Zone(state.ZLibrary, p); len(got) != len(ids) {
		t.Fatalf("precondition: seeded library = %v, want %d cards", got, len(ids))
	}
	return ids
}

// publicReveal returns the first public ids-bearing Note, the shape
// effDigUntil emits for its reveal.
func publicReveal(h *fakeHost) *events.Event {
	for i := range h.log {
		if h.log[i].Kind == events.Note && len(h.log[i].IDs) > 0 && !h.log[i].Secret {
			return &h.log[i]
		}
	}
	return nil
}

// TestDreamHarvestMinTotalCMCCorpusSAExilesZeroMVLibrary is the reported
// symptom on the real card: against a library of 0-mana-value cards, the
// threshold (5) is never reached, so the WHOLE library must be revealed and
// exiled -- not stopped after one card.
func TestDreamHarvestMinTotalCMCCorpusSAExilesZeroMVLibrary(t *testing.T) {
	reg, root := dreamHarvestRoot(t)
	_ = reg

	// Precondition: the real compiled SA carries the threshold and the
	// selector this test relies on.
	if root.Params["MinTotalCMC"] != "5" || root.Params["Valid"] != "Card" ||
		root.Params["FoundDestination"] != "Exile" || root.Params["Defined"] != "Opponent" {
		t.Fatalf("compiled Dream Harvest params = %v, want MinTotalCMC$ 5 / Valid$ Card / FoundDestination$ Exile / Defined$ Opponent",
			root.Params)
	}

	h := newHost(t, 2)
	src := h.g.AddObject(mkCard(t, "Name:Dream Harvest\nTypes:Sorcery\nOracle:x\n"), 0)
	src.Zone = state.ZBattlefield
	h.g.SetZone(state.ZBattlefield, 0, []state.ObjID{src.ID})

	// Seat 1 is the opponent `Defined$ Opponent` names (controller 0).
	var library []*cards.Card
	for _, nm := range []string{"A", "B", "C", "D"} {
		library = append(library, mkCard(t, "Name:"+nm+"\nTypes:Creature\nPT:1/1\nOracle:x\n"))
	}
	lib := seededLibrary(t, h, 1, library...)
	for _, id := range lib {
		if mv := h.g.Obj(id).Face().ManaValue(); mv != 0 {
			t.Fatalf("precondition: %d mana value = %d, want 0 (the 0-MV case never reaches the threshold)", id, mv)
		}
	}

	Resolve(h, &Ctx{Source: src.ID, Controller: 0}, root)

	if got := h.g.Zone(state.ZLibrary, 1); len(got) != 0 {
		t.Fatalf("opponent's library has %d cards left, want 0 (whole 0-MV library exiles)", len(got))
	}
	for _, id := range lib {
		if got := h.g.Obj(id).Zone; got != state.ZExile {
			t.Fatalf("card %d zone = %v, want exile", id, got)
		}
	}
	reveal := publicReveal(h)
	if reveal == nil || len(reveal.IDs) != len(lib) {
		t.Fatalf("reveal note = %+v, want all %d cards revealed", reveal, len(lib))
	}
}

// TestDreamHarvestMinTotalCMCStopsAtCumulativeThreshold pins the cumulative
// semantics: only the MATCHING cards' mana values sum toward the threshold,
// and the scan stops the moment the running total reaches it. With a top-down
// library of mana values 1, 2, 6, 0 the threshold 5 is first reached at the
// third card (1+2+6 = 9), so exactly three cards exile and the fourth stays.
func TestDreamHarvestMinTotalCMCStopsAtCumulativeThreshold(t *testing.T) {
	_, root := dreamHarvestRoot(t)

	h := newHost(t, 2)
	src := h.g.AddObject(mkCard(t, "Name:Dream Harvest\nTypes:Sorcery\nOracle:x\n"), 0)
	src.Zone = state.ZBattlefield
	h.g.SetZone(state.ZBattlefield, 0, []state.ObjID{src.ID})

	merfolk := mkCard(t, "Name:Merfolk of the Pearl Trident\nManaCost:U\nTypes:Creature Merfolk\nPT:1/1\nOracle:x\n")
	bears := mkCard(t, "Name:Grizzly Bears\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	dragon := mkCard(t, "Name:Shivan Dragon\nManaCost:4 R R\nTypes:Creature Dragon\nPT:5/5\nOracle:x\n")
	filler := mkCard(t, zeroMVManaScript)
	lib := seededLibrary(t, h, 1, merfolk, bears, dragon, filler)

	// Precondition: the running total only crosses the threshold if these
	// values are what the test claims (1, 2, 6, 0).
	wantMV := []int32{1, 2, 6, 0}
	for i, id := range lib {
		if mv := h.g.Obj(id).Face().ManaValue(); mv != wantMV[i] {
			t.Fatalf("precondition: library[%d] mana value = %d, want %d", i, mv, wantMV[i])
		}
	}

	Resolve(h, &Ctx{Source: src.ID, Controller: 0}, root)

	for i, want := range []state.Zone{state.ZExile, state.ZExile, state.ZExile, state.ZLibrary} {
		if got := h.g.Obj(lib[i]).Zone; got != want {
			t.Fatalf("library[%d] zone = %v, want %v (1+2+6 = 9 >= 5 at the third card)", i, got, want)
		}
	}
	if got := h.g.Zone(state.ZLibrary, 1); len(got) != 1 || got[0] != lib[3] {
		t.Fatalf("opponent's library = %v, want only the 0-MV filler %d", got, lib[3])
	}
	reveal := publicReveal(h)
	if reveal == nil || len(reveal.IDs) != 3 {
		t.Fatalf("reveal note = %+v, want exactly the 3 cards that reached the threshold", reveal)
	}
}
