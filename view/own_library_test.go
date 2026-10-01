package view

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// libraryCards parses one named vanilla creature, for a library whose contents
// (and order) the test controls exactly.
func libraryCards(t *testing.T, names ...string) []*cards.Card {
	t.Helper()
	out := make([]*cards.Card, 0, len(names))
	for _, n := range names {
		c, err := cards.ParseBytes("lib.txt", []byte("Name:"+n+"\nManaCost:1 G\nTypes:Creature\nPT:1/1\nOracle:x\n"))
		if err != nil {
			t.Fatalf("parse %s: %v", n, err)
		}
		c.Link()
		out = append(out, c)
	}
	return out
}

func libraryNames(cvs []CardView) []string {
	out := make([]string, 0, len(cvs))
	for _, c := range cvs {
		out = append(out, c.Name)
	}
	return out
}

// TestOwnLibraryIsProjectedUnordered pins the own-seat library projection:
// the viewer reads their own library as a []CardView in a canonical, order-free
// arrangement (by name), every other seat's Library is nil (CR 400.2), and a
// spectator sees none. The fixture deliberately sets the secret library order
// to something that is NOT the canonical order, so a projection that merely
// echoed the library would fail rather than pass by coincidence.
func TestOwnLibraryIsProjectedUnordered(t *testing.T) {
	g := state.NewGame([]string{"alice", "bob", "carol"})
	// Seat 0's library order is Zebra, Apple, Mango -- chosen so the secret
	// order differs from the canonical (name-sorted) Apple, Mango, Zebra.
	seedCards := libraryCards(t, "Zebra", "Apple", "Mango")
	lib := make([]state.ObjID, 0, len(seedCards))
	for _, c := range seedCards {
		lib = append(lib, g.AddObject(c, 0).ID)
	}
	g.SetZone(state.ZLibrary, 0, lib)

	// Seat 1 carries two cards, so "no library" cannot pass on an empty one.
	otherCards := libraryCards(t, "Bear", "Wolf")
	other := make([]state.ObjID, 0, len(otherCards))
	for _, c := range otherCards {
		other = append(other, g.AddObject(c, 1).ID)
	}
	g.SetZone(state.ZLibrary, 1, other)

	// Precondition: the secret order is not the canonical order, so the
	// sorted assertion below is meaningful (it can fail).
	secret := []string{"Zebra", "Apple", "Mango"}
	if secret[0] == "Apple" {
		t.Fatal("fixture invalid: secret order already canonical; the order-free assertion could not fail")
	}

	// Viewer 0 sees its own library, canonicalised by name.
	v0 := Project(g, flatChars{g}, 0, nil)
	pv0 := v0.Players[0]
	if pv0.Library == nil {
		t.Fatal("own seat sees no library: own_library_list not projected")
	}
	if len(pv0.Library) != 3 {
		t.Fatalf("own library size = %d, want 3 (%v)", len(pv0.Library), libraryNames(pv0.Library))
	}
	// LibrarySize (the count) must still agree with the list.
	if pv0.LibrarySize != len(pv0.Library) {
		t.Fatalf("LibrarySize=%d disagrees with Library len=%d", pv0.LibrarySize, len(pv0.Library))
	}
	want := []string{"Apple", "Mango", "Zebra"}
	got := libraryNames(pv0.Library)
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("own library order = %v, want canonical %v (secret order must not leak)", got, want)
		}
	}
	// The projection must NOT preserve the secret library order.
	if got[0] == "Zebra" {
		t.Fatalf("own library leaked its secret order: %v", got)
	}

	// Every other seat's library is hidden: nil, not an empty slice.
	v0other := Project(g, flatChars{g}, 0, nil)
	for _, id := range []state.PlayerID{1, 2} {
		if v0other.Players[id].Library != nil {
			t.Fatalf("viewer 0 sees seat %d library: %v", id, libraryNames(v0other.Players[id].Library))
		}
	}
	// A count stays public even when the contents do not.
	if v0other.Players[1].LibrarySize != 2 {
		t.Fatalf("seat 1 LibrarySize = %d, want 2 (a count stays public)", v0other.Players[1].LibrarySize)
	}
	if v0other.Players[2].LibrarySize != 0 {
		t.Fatalf("seat 2 LibrarySize = %d, want 0", v0other.Players[2].LibrarySize)
	}

	// Viewer 1 sees its own library but not seat 0's.
	v1 := Project(g, flatChars{g}, 1, nil)
	if len(v1.Players[1].Library) != 2 {
		t.Fatalf("viewer 1 reads no own library: %v", libraryNames(v1.Players[1].Library))
	}
	if v1.Players[0].Library != nil {
		t.Fatalf("viewer 1 sees seat 0 library: %v", libraryNames(v1.Players[0].Library))
	}

	// A spectator (viewer naming no real seat) sees no one's library.
	vs := Project(g, flatChars{g}, 99, nil)
	for _, pv := range vs.Players {
		if pv.Library != nil {
			t.Fatalf("spectator sees seat %d library: %v", pv.ID, libraryNames(pv.Library))
		}
	}
}
