package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestLibrarySearchGainControlTrueHandsPermanentToCaster pins the CR 610
// one-shot control change on the hidden-library ChangeZone mover the
// AGENTS.md row names (`effects/zone.go` `applyLibrarySearch`): Bribery
// searches a target opponent's library and puts the found creature onto the
// battlefield under the CASTER's control, so the fetched permanent must end
// up controlled by the resolving controller (seat 0), not by the library's
// owner (seat 1) the plain MoveZone would leave it with.
//
// The SA is the real corpus Bribery ability (`GainControl$ True`); the
// fetched permanent is the real corpus Grizzly Bears.
func TestLibrarySearchGainControlTrueHandsPermanentToCaster(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	if reg == nil {
		t.Skip("library-gaincontrol corpus unavailable")
	}
	bribery, ok := reg.Lookup("Bribery")
	if !ok {
		t.Fatal("missing corpus Bribery")
	}
	bears, ok := reg.Lookup("Grizzly Bears")
	if !ok {
		t.Fatal("missing corpus Grizzly Bears")
	}
	briberySA := bribery.Faces[0].Abilities[0]
	if briberySA.API != "ChangeZone" || briberySA.Params["Origin"] != "Library" ||
		briberySA.Params["GainControl"] != "True" {
		t.Fatalf("Bribery SA = %q api=%s origin=%q gain=%q, want the corpus library search with GainControl$ True",
			briberySA.Line, briberySA.API, briberySA.Params["Origin"], briberySA.Params["GainControl"])
	}

	h := newHost(t, 2)
	src := h.g.AddObject(bribery, 0)
	fetched := h.g.AddObject(bears, 1)
	h.g.Obj(fetched.ID).Zone = state.ZLibrary
	h.g.SetZone(state.ZLibrary, 1, []state.ObjID{fetched.ID})

	// Precondition: the fetched permanent sits in seat 1's library, owned and
	// controlled by seat 1 -- which differs from the GainControl$ target
	// (seat 0). A vacuous setup (already under the caster, or not in the
	// library) must fail loudly rather than pass silently.
	if got := h.g.Obj(fetched.ID).Zone; got != state.ZLibrary {
		t.Fatalf("precondition: fetched permanent zone=%s, want library", got)
	}
	if h.g.Obj(fetched.ID).Owner != 1 || h.g.Obj(fetched.ID).Controller != 1 {
		t.Fatalf("precondition: fetched owner=%d controller=%d, want 1/1",
			h.g.Obj(fetched.ID).Owner, h.g.Obj(fetched.ID).Controller)
	}

	applyLibrarySearch(h, &Ctx{Controller: 0, Source: src.ID}, briberySA, ChangeZoneOf(briberySA),
		1, state.ZBattlefield, []state.ObjID{fetched.ID}, []state.Zone{state.ZLibrary})

	if got := h.g.Obj(fetched.ID).Zone; got != state.ZBattlefield {
		t.Fatalf("fetched permanent zone=%s, want battlefield", got)
	}
	if got := h.g.Obj(fetched.ID).Controller; got != 0 {
		t.Fatalf("fetched permanent controller=%d, want 0 (the GainControl$ caster)", got)
	}
	moveIdx, controlIdx := -1, -1
	for i, ev := range h.log {
		switch ev.Kind {
		case events.MoveZone:
			if ev.Obj == fetched.ID {
				moveIdx = i
			}
		case events.ControlChange:
			if ev.Obj == fetched.ID {
				controlIdx = i
				if ev.Player != 0 {
					t.Fatalf("ControlChange player=%d, want 0", ev.Player)
				}
			}
		}
	}
	if moveIdx < 0 {
		t.Fatalf("no MoveZone for the fetched permanent: %+v", h.log)
	}
	if controlIdx < 0 {
		t.Fatalf("no ControlChange for the fetched permanent: %+v", h.log)
	}
	if moveIdx > controlIdx {
		t.Fatalf("ControlChange (%d) preceded the MoveZone (%d); control must be recorded after the entry", controlIdx, moveIdx)
	}
}

// TestLibrarySearchGainControlTargetedHandsPermanentToNamedPlayer pins the
// same mover for a GainControl$ selector rather than the bare True: Yavimaya
// Dryad's "put it onto the battlefield tapped under TARGET PLAYER's control"
// carries GainControl$ Targeted, so the fetched Forest must be controlled by
// the chosen target player (seat 1), not by the library's owner (seat 0, the
// caster).
func TestLibrarySearchGainControlTargetedHandsPermanentToNamedPlayer(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	if reg == nil {
		t.Skip("library-gaincontrol corpus unavailable")
	}
	dryad, sa := corpusSA(t, "Yavimaya Dryad", "DBChangeZone")
	forest, ok := reg.Lookup("Forest")
	if !ok {
		t.Fatal("missing corpus Forest")
	}
	if sa.Params["Origin"] != "Library" || sa.Params["GainControl"] != "Targeted" {
		t.Fatalf("DBChangeZone origin=%q gain=%q, want the corpus library search with GainControl$ Targeted",
			sa.Params["Origin"], sa.Params["GainControl"])
	}

	h := newHost(t, 2)
	src := h.g.AddObject(dryad, 0)
	fetched := h.g.AddObject(forest, 0)
	h.g.Obj(fetched.ID).Zone = state.ZLibrary
	h.g.SetZone(state.ZLibrary, 0, []state.ObjID{fetched.ID})

	// Precondition: the fetched land is in seat 0's library, controlled by
	// its owner seat 0, and the target player (seat 1) differs from that
	// owner. Without the difference the assertion could not fail.
	if got := h.g.Obj(fetched.ID).Zone; got != state.ZLibrary {
		t.Fatalf("precondition: fetched land zone=%s, want library", got)
	}
	if h.g.Obj(fetched.ID).Controller != 0 {
		t.Fatalf("precondition: fetched controller=%d, want 0", h.g.Obj(fetched.ID).Controller)
	}
	if h.g.Obj(fetched.ID).Controller == 1 {
		t.Fatal("precondition: fetched permanent already controlled by the target player")
	}

	applyLibrarySearch(h, &Ctx{Controller: 0, Source: src.ID,
		Targets: []state.Target{{Player: 1, IsPlayer: true}}}, sa, ChangeZoneOf(sa),
		0, state.ZBattlefield, []state.ObjID{fetched.ID}, []state.Zone{state.ZLibrary})

	if got := h.g.Obj(fetched.ID).Zone; got != state.ZBattlefield {
		t.Fatalf("fetched land zone=%s, want battlefield", got)
	}
	if got := h.g.Obj(fetched.ID).Controller; got != 1 {
		t.Fatalf("fetched land controller=%d, want 1 (the GainControl$ Targeted player)", got)
	}
	found := false
	for _, ev := range h.log {
		if ev.Kind == events.ControlChange && ev.Obj == fetched.ID {
			found = true
			if ev.Player != 1 {
				t.Fatalf("ControlChange player=%d, want 1", ev.Player)
			}
		}
	}
	if !found {
		t.Fatalf("no ControlChange for the fetched land: %+v", h.log)
	}
}
