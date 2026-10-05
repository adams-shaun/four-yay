package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestDiscoverDefinedTargetedOwnerUsesOwnersLibrary pins Zoyowa's Justice's
// `Defined$ TargetedOwner` shape: the discover scans the OWNER of the resolving
// ability's target, not the resolving controller (CR 701.57: "The owner of
// target artifact ... Then that player discovers X").  Before the fix
// effDiscover hard-coded c.Controller, so the wrong seat's library was exiled,
// the marker named the wrong seat, and the target owner kept every card
// (std3 LCI: p1.library_count gorge "39", xmage "38").
func TestDiscoverDefinedTargetedOwnerUsesOwnersLibrary(t *testing.T) {
	h := newHost(t, 2)
	// Distinguishable libraries: p0's cards are named so a wrong-library scan
	// is visible, p1's carry the card the discover can find.
	p0Card := mkCard(t, "Name:P0 Card\nManaCost:no cost\nTypes:Land\nOracle:x\n")
	p1Found := mkCard(t, "Name:P1 Found\nManaCost:1\nTypes:Creature\nPT:1/1\nOracle:x\n")
	_ = fillLibrary(h.g, 0, p0Card, 3)
	p1Lib := fillLibrary(h.g, 1, p1Found, 4)

	// The resolving source is controlled by p0; the targeted permanent is
	// owned by p1 (Sol Ring in the oracle scenario).
	source := h.g.AddObject(mkCard(t, "Name:Zoyowa's Justice\nManaCost:1 R\nTypes:Instant\nOracle:x\n"), 0)
	h.Emit(events.Event{Kind: events.MoveZone, Obj: source.ID, From: state.ZLibrary, To: state.ZBattlefield})
	target := h.g.AddObject(mkCard(t, "Name:Sol Ring\nManaCost:1\nTypes:Artifact\nOracle:x\n"), 1)
	h.Emit(events.Event{Kind: events.MoveZone, Obj: target.ID, From: state.ZLibrary, To: state.ZBattlefield})

	// Preconditions the assertion depends on: the target is on the
	// battlefield and owned by seat 1, and the two libraries differ.
	if o := h.g.Obj(target.ID); o == nil || o.Zone != state.ZBattlefield || o.Owner != 1 {
		t.Fatalf("precondition target = %+v, want battlefield owner 1", o)
	}
	if len(fillLibraryIDs(h.g, 0)) == len(p1Lib) {
		t.Fatal("precondition p0 and p1 libraries are indistinguishable")
	}

	c := &Ctx{Source: source.ID, Controller: 0, Targets: []state.Target{{Obj: target.ID}}}
	Resolve(h, c, sa(t, "SP$ Discover | Defined$ TargetedOwner | Num$ 1"))

	// The first exiles must be p1's library cards, never p0's.
	var exiledFromP0, exiledFromP1 bool
	var marker *events.Event
	for i := range h.log {
		ev := &h.log[i]
		if ev.Kind == events.MoveZone && ev.From == state.ZLibrary && ev.To == state.ZExile {
			if inIDs(p1Lib, ev.Obj) {
				exiledFromP1 = true
			} else {
				exiledFromP0 = true
			}
		}
		if ev.Kind == events.Discover {
			marker = ev
		}
	}
	if exiledFromP0 {
		t.Fatalf("discover exiled a p0 library card; it must scan the owner (p1)")
	}
	if !exiledFromP1 {
		t.Fatalf("discover exiled no p1 library card; log = %+v", h.log)
	}
	if marker == nil {
		t.Fatal("no Discover marker emitted")
	}
	if marker.Player != 1 {
		t.Fatalf("Discover marker player = %d, want 1 (the target's owner)", marker.Player)
	}
}

// fillLibraryIDs returns the current library slice for a seat.
func fillLibraryIDs(g *state.Game, p state.PlayerID) []state.ObjID {
	return append([]state.ObjID(nil), g.Zone(state.ZLibrary, p)...)
}

// inIDs reports whether id is in ids.
func inIDs(ids []state.ObjID, id state.ObjID) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}
