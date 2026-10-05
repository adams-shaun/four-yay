package effects

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestChangeZoneThisDefinedAndTgtsRealCorpus pins Suspend Aggression's real
// ChangeZone script: fixing commit 73c9cc2e5 makes TopOfLibrary join the
// targeted nonland permanent in the exile move.
func TestChangeZoneThisDefinedAndTgtsRealCorpus(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	card, ok := reg.Lookup("Suspend Aggression")
	if !ok {
		t.Fatal("corpus pin moved: Suspend Aggression is missing")
	}
	if len(card.Faces) == 0 || len(card.Faces[0].Abilities) == 0 {
		t.Fatal("corpus pin moved: Suspend Aggression has no root ability")
	}
	face := card.Faces[0]
	root := face.Abilities[0]
	if root == nil || root.API != "ChangeZone" {
		if root == nil {
			t.Fatal("corpus pin moved: Suspend Aggression root ability is nil")
		}
		t.Fatalf("corpus pin moved: root API = %q, want ChangeZone", root.API)
	}

	h := newHost(t, 2)
	src := h.g.AddObject(card, 0)
	src.Zone = state.ZBattlefield
	h.g.SetZone(state.ZBattlefield, 0, []state.ObjID{src.ID})

	victim := h.g.AddObject(mkCard(t, "Name:Victim\nTypes:Artifact\nOracle:x\n"), 1).ID
	h.g.Obj(victim).Zone = state.ZBattlefield
	h.g.SetZone(state.ZBattlefield, 1, []state.ObjID{victim})
	if h.g.Obj(victim).Zone != state.ZBattlefield || len(h.g.Zone(state.ZBattlefield, 1)) != 1 || h.g.Zone(state.ZBattlefield, 1)[0] != victim {
		t.Fatal("precondition: target nonland permanent must be on seat 1's battlefield")
	}

	top := h.g.AddObject(mkCard(t, "Name:TopCard\nTypes:Creature\nPT:1/1\nOracle:x\n"), 0).ID
	below := h.g.AddObject(mkCard(t, "Name:Below\nTypes:Creature\nPT:1/1\nOracle:x\n"), 0).ID
	h.g.SetZone(state.ZLibrary, 0, []state.ObjID{top, below})
	library := h.g.Zone(state.ZLibrary, 0)
	if len(library) != 2 || library[0] != top || library[1] != below {
		t.Fatalf("precondition: controller's library = %v, want top %d then %d", library, top, below)
	}
	if h.g.Obj(top).Zone != state.ZLibrary || h.g.Obj(below).Zone != state.ZLibrary || top == below {
		t.Fatal("precondition: distinct top and second cards must be in the library")
	}

	Resolve(h, &Ctx{Source: src.ID, Controller: 0, SVars: face.SVars,
		Targets: []state.Target{{Obj: victim}}}, root)

	if got := h.g.Obj(victim).Zone; got != state.ZExile {
		t.Fatalf("target nonland permanent zone = %v, want exile; notes %v", got, ufNotes(h))
	}
	if got := h.g.Obj(top).Zone; got != state.ZExile {
		t.Fatalf("controller's top library card zone = %v, want exile; notes %v", got, ufNotes(h))
	}
	if got := h.g.Obj(below).Zone; got != state.ZLibrary {
		t.Fatalf("second library card zone = %v, want library", got)
	}
	for _, note := range ufNotes(h) {
		if strings.Contains(note, "ThisDefinedAndTgts") {
			t.Fatalf("real ThisDefinedAndTgts$ ability emitted a note: %q", note)
		}
	}
}
