package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// dreadBoard builds a two-seat board with a resolving source permanent on
// seat 0's battlefield and the given libraries on each seat. It returns the
// host, the source id, and the two libraries (index 0 = top).
func dreadBoard(t *testing.T, p0, p1 []string) (*askHost, state.ObjID, []state.ObjID, []state.ObjID) {
	t.Helper()
	h := &askHost{}
	h.g = state.NewGame(names(2))
	src := h.g.AddObject(mkCard(t, "Name:Relic\nTypes:Artifact\nOracle:x\n"), 0).ID
	h.g.SetZone(state.ZBattlefield, 0, []state.ObjID{src})
	add := func(p state.PlayerID, specs []string) []state.ObjID {
		var ids []state.ObjID
		for _, s := range specs {
			ids = append(ids, h.g.AddObject(mkCard(t, s), p).ID)
		}
		h.g.SetZone(state.ZLibrary, p, ids)
		return ids
	}
	return h, src, add(0, p0), add(1, p1)
}

func dreadFaceDownOnBf(h *askHost, id state.ObjID) bool {
	o := h.g.Obj(id)
	return o != nil && o.Zone == state.ZBattlefield
}

// TestManifestDreadRepeatsAmountTimes pins Forge's Amount$ as a REPEAT
// count, not a window size: `Amount$ 2` runs the whole two-card operation
// twice, so four cards are consumed and two permanents land face down. The
// old build treated Amount as the window size and ran once.
func TestManifestDreadRepeatsAmountTimes(t *testing.T) {
	h, src, lib, _ := dreadBoard(t,
		[]string{riderBear, riderLand, riderBear, riderLand}, nil)
	sa := &cards.SA{API: "ManifestDread", Params: map[string]string{"Amount": "2"}}
	effManifestDread(h, &Ctx{Source: src, Controller: 0}, sa)
	// Precondition: the library really held a full pair per iteration and
	// the four cards are distinct, so "four consumed" cannot be a pair
	// counted twice.
	seen := map[state.ObjID]bool{}
	for _, id := range lib {
		if seen[id] {
			t.Fatalf("duplicate library id %d", id)
		}
		seen[id] = true
	}
	bf := 0
	gy := 0
	for _, id := range lib {
		o := h.g.Obj(id)
		if o == nil {
			continue
		}
		switch {
		case dreadFaceDownOnBf(h, id):
			bf++
		case o.Zone == state.ZGraveyard:
			gy++
		}
	}
	if bf != 2 || gy != 2 {
		t.Fatalf("Amount$ 2 consumed %d face-down permanents and %d graveyard cards, want 2 and 2; log=%+v", bf, gy, h.log)
	}
}

// TestManifestDreadDefinedPlayerLibrary pins DefinedPlayer$: the named
// player's library is the one looked at, not the resolving controller's.
func TestManifestDreadDefinedPlayerLibrary(t *testing.T) {
	h, src, lib0, lib1 := dreadBoard(t,
		[]string{riderBear, riderLand}, []string{riderBear, riderLand})
	sa := &cards.SA{API: "ManifestDread", Params: map[string]string{"DefinedPlayer": "Opponent"}}
	effManifestDread(h, &Ctx{Source: src, Controller: 0}, sa)
	// Precondition: seat 1 (the opponent) really has a library, so the
	// assertion below is about the selector and not an empty library.
	if len(lib1) != 2 {
		t.Fatalf("opponent library not set up: %v", lib1)
	}
	movedOpp := 0
	for _, id := range lib1 {
		if o := h.g.Obj(id); o != nil && o.Zone != state.ZLibrary {
			movedOpp++
		}
	}
	if movedOpp != 2 {
		t.Errorf("DefinedPlayer$ Opponent left the opponent's library untouched (%d moved); log=%+v", movedOpp, h.log)
	}
	for _, id := range lib0 {
		if o := h.g.Obj(id); o == nil || o.Zone != state.ZLibrary {
			t.Errorf("controller's card %d was touched by a DefinedPlayer$ Opponent dread", id)
		}
	}
}

// TestManifestDreadRemembersManifested pins RememberManifested$ True: the
// manifested object joins the source's event-backed Remembered list so a
// chained DBAttach/DBPutCounter reading Defined$ Remembered finds it.
func TestManifestDreadRemembersManifested(t *testing.T) {
	h, src, lib, _ := dreadBoard(t, []string{riderBear, riderLand}, nil)
	sa := &cards.SA{API: "ManifestDread", Params: map[string]string{"RememberManifested": "True"}}
	effManifestDread(h, &Ctx{Source: src, Controller: 0}, sa)
	// Precondition: a card was actually manifested, so the remembrance
	// assertion cannot pass on nothing.
	var manifested state.ObjID
	for _, id := range lib {
		if dreadFaceDownOnBf(h, id) {
			manifested = id
		}
	}
	if manifested == 0 {
		t.Fatalf("no card manifested; log=%+v", h.log)
	}
	o := h.g.Obj(src)
	if o == nil {
		t.Fatalf("source gone")
	}
	found := false
	for _, r := range o.Remembered {
		if r.Obj == manifested {
			found = true
		}
	}
	if !found {
		t.Errorf("manifested %d not in source Remembered %v", manifested, o.Remembered)
	}
	chose := 0
	for _, e := range h.log {
		if e.Kind == events.Choose && e.Obj == src && e.Counter == "remembered" {
			chose++
		}
	}
	if chose != 1 {
		t.Errorf("remembered Choose events = %d, want 1", chose)
	}
}

// TestManifestDreadWithoutRememberIsSilent is the companion: absent
// RememberManifested$, the manifested object is NOT remembered, and the
// dread still ran (a card is face down, no unimplemented note), so the test
// above is about the param and not a disabled handler.
func TestManifestDreadWithoutRememberIsSilent(t *testing.T) {
	h, src, lib, _ := dreadBoard(t, []string{riderBear, riderLand}, nil)
	sa := &cards.SA{API: "ManifestDread", Params: map[string]string{}}
	effManifestDread(h, &Ctx{Source: src, Controller: 0}, sa)
	manifested := 0
	for _, id := range lib {
		if dreadFaceDownOnBf(h, id) {
			manifested++
		}
	}
	if manifested != 1 {
		t.Fatalf("dread did not run: %d face-down permanents; log=%+v", manifested, h.log)
	}
	if o := h.g.Obj(src); len(o.Remembered) != 0 {
		t.Errorf("dread without RememberManifested$ remembered %v", o.Remembered)
	}
}
