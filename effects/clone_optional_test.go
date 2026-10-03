package effects

// The Optional$ True election leaves of DB$ Clone (ticket
// api-clone-trigger-copy, Sarkhan Soul Aflame's shape): the no-host stand-in
// and the Ctx answer-field contract the rules-side resume arm depends on.

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// cloneOptionalSA is Sarkhan Soul Aflame's compiled clone body verbatim.
const cloneOptionalSA = "DB$ Clone | Defined$ TriggeredCardLKICopy | NewName$ Sarkhan, Soul Aflame | AddTypes$ Legendary | Duration$ UntilEndOfTurn | Optional$ True"

// cloneOptionalFixture puts a Sarkhan-like permanent (the become operand,
// the SA's default CloneTarget$ Self) and a Dragon-like body (the copy
// source) on seat 0's battlefield and returns (host, sarkID, dragonID). The
// ctx the caller passes carries the Dragon in Remembered, exactly what the
// rules engine binds for a ChangesZone trigger's TriggeredCardLKICopy.
func cloneOptionalFixture(t *testing.T) (*fakeHost, state.ObjID, state.ObjID) {
	t.Helper()
	h := &fakeHost{}
	h.g = state.NewGame(names(2))
	sark := mkCard(t, "Name:Sarkhan, Soul Aflame\nManaCost:1 U R\nTypes:Legendary Creature Human Shaman\nPT:2/4\nOracle:x\n")
	dragon := mkCard(t, "Name:Dragon Hatchling\nManaCost:1 R\nTypes:Creature Dragon\nPT:0/1\nK:Flying\nOracle:x\n")
	sarkID := h.g.AddObject(sark, 0).ID
	dragonID := h.g.AddObject(dragon, 0).ID
	h.g.SetZone(state.ZBattlefield, 0, []state.ObjID{sarkID, dragonID})
	// SetZone moves the zone LIST only; the object's cached Zone field is
	// what effClone's become-object check reads (the attach_test convention).
	h.g.Obj(sarkID).Zone = state.ZBattlefield
	h.g.Obj(dragonID).Zone = state.ZBattlefield
	return h, sarkID, dragonID
}

// TestCloneOptionalNoHostTakesTheCopy pins the R-9 no-host stand-in: a host
// that cannot ask resolves the "you may" copy deterministically as take,
// with the one degradation Note and the same layer-1 copy the pre-election
// build performed.
func TestCloneOptionalNoHostTakesTheCopy(t *testing.T) {
	h, sark, dragon := cloneOptionalFixture(t)
	ctx := &Ctx{Controller: 0, Source: sark, Remembered: []state.Target{{Obj: dragon}}}
	Resolve(h, ctx, sa(t, cloneOptionalSA))
	f := h.g.Obj(sark).Face()
	if f == nil || f.Name != "Sarkhan, Soul Aflame" {
		t.Fatalf("no-host copy name %v, want the overridden Sarkhan, Soul Aflame", f)
	}
	if h.g.Obj(sark).CopyFace == nil {
		t.Fatal("no-host run recorded no copy basis")
	}
	took := false
	for _, e := range h.log {
		if e.Kind == events.Note && e.Text == "Clone Optional$ resolved as take (no engine host to ask)" {
			took = true
		}
	}
	if !took {
		t.Fatal("no-host take ran without its degradation Note")
	}
}

func hasNote(h *fakeHost, text string) bool {
	for _, e := range h.log {
		if e.Kind == events.Note && e.Text == text {
			return true
		}
	}
	return false
}

func cloneNoteTexts(h *fakeHost) []string {
	var out []string
	for _, e := range h.log {
		if e.Kind == events.Note {
			out = append(out, e.Text)
		}
	}
	return out
}
