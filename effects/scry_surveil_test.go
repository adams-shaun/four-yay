package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestScryNoHostStandInKeepsOrder is the Ruling J6.4 leaf for Scry: a host
// that cannot ask (the effects-package test double, whose Ask always returns
// false) falls back to the deterministic stand-in of keeping every card on
// top in its existing order -- pile B empty, nothing to the bottom -- and the
// resolution completes rather than wedging.
func TestScryNoHostStandInKeepsOrder(t *testing.T) {
	h := newHost(t, 2)
	card := mkCard(t, "Name:C\nTypes:Creature\nPT:1/1\nOracle:x\n")
	ids := fillLibrary(h.g, 0, card, 5)
	before := append([]state.ObjID(nil), h.g.Zone(state.ZLibrary, 0)...)
	if len(ids) != 5 {
		t.Fatalf("set up %d library cards, want 5", len(ids))
	}

	Resolve(h, &Ctx{Source: 1, Controller: 0}, sa(t, "SP$ Scry | Defined$ You | ScryNum$ 3"))

	after := h.g.Zone(state.ZLibrary, 0)
	if len(after) != len(before) {
		t.Fatalf("library size %d -> %d after the no-host scry", len(before), len(after))
	}
	for i := range before {
		if after[i] != before[i] {
			t.Fatalf("no-host scry reordered the library at index %d: %v -> %v; the stand-in must keep the existing order", i, before[i], after[i])
		}
	}
	// The stand-in emits a LibraryOrder carrying the full, unchanged order,
	// and no MoveZone (nothing left the library).
	found := false
	for _, ev := range h.log {
		switch ev.Kind {
		case events.LibraryOrder:
			found = true
			if ev.Player != 0 || !ev.Secret {
				t.Fatalf("stand-in LibraryOrder event must name the owner and be Secret: %+v", ev)
			}
			if len(ev.IDs) != len(before) {
				t.Fatalf("stand-in LibraryOrder carried %d ids, want the full %d-card order", len(ev.IDs), len(before))
			}
		case events.MoveZone:
			t.Fatalf("no-host scry emitted a MoveZone %+v; the stand-in must move nothing", ev)
		}
	}
	if !found {
		t.Fatal("no LibraryOrder event emitted by the no-host scry stand-in")
	}
	if h.Suspended() {
		t.Fatal("the effects test double reported a suspension; a no-host host must not wedge")
	}
}

// TestSurveilNoHostStandInPutsNothingInGraveyard is the Ruling J6.4 leaf for
// Surveil: a host that cannot ask leaves every card on top (nothing to the
// graveyard) and emits no MoveZone.
func TestSurveilNoHostStandInPutsNothingInGraveyard(t *testing.T) {
	h := newHost(t, 2)
	card := mkCard(t, "Name:C\nTypes:Creature\nPT:1/1\nOracle:x\n")
	fillLibrary(h.g, 0, card, 5)
	before := append([]state.ObjID(nil), h.g.Zone(state.ZLibrary, 0)...)

	Resolve(h, &Ctx{Source: 1, Controller: 0}, sa(t, "SP$ Surveil | Defined$ You | Amount$ 2"))

	after := h.g.Zone(state.ZLibrary, 0)
	for i := range before {
		if after[i] != before[i] {
			t.Fatalf("no-host surveil reordered the library at index %d: %v -> %v; the stand-in must keep the existing order", i, before[i], after[i])
		}
	}
	if gy := h.g.Zone(state.ZGraveyard, 0); len(gy) != 0 {
		t.Fatalf("no-host surveil left %d card(s) in the graveyard, want 0 (nothing goes there)", len(gy))
	}
	for _, ev := range h.log {
		if ev.Kind == events.MoveZone {
			t.Fatalf("no-host surveil emitted a MoveZone %+v; the stand-in must move nothing", ev)
		}
	}
	found := false
	for _, ev := range h.log {
		if ev.Kind == events.LibraryOrder {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("no LibraryOrder event emitted by the no-host surveil stand-in")
	}
}

// countSurveilMarkers reports how many events.Surveil records the effects
// host's log carries.
func countSurveilMarkers(h *suspendHost) int {
	n := 0
	for _, ev := range h.log {
		if ev.Kind == events.Surveil {
			n++
		}
	}
	return n
}
