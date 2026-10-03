package effects

// The explicit Optional$ confirm-before-search gate for the hidden-library
// ChangeZone search (effSearchLibrary): Forge's ChangeZoneEffect
// changeHiddenOriginResolve poses its confirmAction gate BEFORE the fetch
// list is consulted, so an Optional$ True/You library search must ask the
// search player whether to proceed before it offers (or fails to find) a
// card. The marker is read through the ONE shared optionalConfirmMarker the
// hidden-hand walk and the Hidden$ True pick already use. A decline skips
// that player's search, card pick and search-specific shuffle/tail; an
// accepted confirmation enters the ordinary Min-0/mandatory search and its
// shuffle path unchanged, even when the pool is empty. ChoiceOptional$ and a
// markerless text-may search stay confirmation-free, and the object-valued
// Defined$ fetch list keeps its own election in moveDefinedLibraryObjects
// with no second confirm.

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

const olscOptional = "DB$ ChangeZone | Origin$ Library | Destination$ Hand | ChangeType$ Creature | ChangeNum$ 1 | Optional$ True"

// olscFixtureNoHost builds a 2-seat board whose source searches seat 0's own
// library (no DefinedPlayer$), holding two creatures. It returns a fresh
// askHost, the source id and the two creature ids in library order.
func olscFixture(t *testing.T) (*askHost, state.ObjID, []state.ObjID) {
	t.Helper()
	h := &askHost{}
	h.g = state.NewGame(names(2))
	src := h.g.AddObject(mkCard(t, "Name:Searcher\nTypes:Sorcery\nOracle:x\n"), 0)
	bear := h.g.AddObject(mkCard(t, "Name:Grizzly Bears\nTypes:Creature\nPT:2/2\nOracle:x\n"), 0)
	bird := h.g.AddObject(mkCard(t, "Name:Birds of Paradise\nTypes:Creature\nPT:0/1\nOracle:x\n"), 0)
	ids := []state.ObjID{bear.ID, bird.ID}
	h.g.SetZone(state.ZHand, 0, []state.ObjID{src.ID})
	h.g.SetZone(state.ZLibrary, 0, ids)
	h.g.Obj(src.ID).Zone = state.ZHand
	for _, id := range ids {
		h.g.Obj(id).Zone = state.ZLibrary
	}
	return h, src.ID, ids
}

// TestOptionalLibrarySearchNoHost pins R-9: a host with no decision channel
// plays the may as do (accept), then the search path's existing no-host pick
// policy runs.
func TestOptionalLibrarySearchNoHost(t *testing.T) {
	h, src, ids := olscFixture(t)
	fh := &fakeHost{g: h.g}
	Resolve(fh, &Ctx{Source: src, Controller: 0}, sa(t, olscOptional))
	// Both asks were built and answered deterministically in the player's
	// place: the confirmation gate and the Mandatory-ish Min pick.
	if fh.askCount != 2 {
		t.Fatalf("no-host ask count = %d, want 2 (the confirmation gate and the pick)", fh.askCount)
	}
	// A stated-quality search's no-host stand-in legitimately finds nothing,
	// so accept-then-empty leaves both creatures in the library. The point is
	// that the confirmation did not abort the fetch: the pick ask was posed.
	if fh.lastAsk == nil || fh.lastAsk.ResumeKind != "search" {
		t.Fatalf("no-host last ask = %+v, want the search pick after the accepted confirmation", fh.lastAsk)
	}
	for i, id := range ids {
		if o := fh.g.Obj(id); o.Zone != state.ZLibrary {
			t.Fatalf("no-host stated-quality search moved ids[%d] to %s", i, o.Zone)
		}
	}
	// A quantity-only Optional$ search has a real no-host take: accept then
	// find the deterministic first card.
	qh, qsrc, qids := olscFixture(t)
	qfh := &fakeHost{g: qh.g}
	Resolve(qfh, &Ctx{Source: qsrc, Controller: 0},
		sa(t, "DB$ ChangeZone | Origin$ Library | Destination$ Hand | ChangeType$ Card | ChangeNum$ 1 | Optional$ True"))
	if qfh.askCount != 2 {
		t.Fatalf("quantity no-host ask count = %d, want 2 (confirm + pick)", qfh.askCount)
	}
	if o := qfh.g.Obj(qids[0]); o.Zone != state.ZHand {
		t.Fatalf("quantity no-host search left the first card on %s, want hand", o.Zone)
	}
	if o := qfh.g.Obj(qids[1]); o.Zone != state.ZLibrary {
		t.Fatalf("quantity no-host search moved the second card to %s", o.Zone)
	}
}
