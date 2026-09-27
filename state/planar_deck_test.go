package state

import "testing"

func TestPlanarDeckZoneIsPerSeatAndCloned(t *testing.T) {
	g := NewGame([]string{"A", "B"})
	a := g.AddObject(nil, 0)
	b := g.AddObject(nil, 1)
	a.Zone, b.Zone = ZPlanarDeck, ZPlanarDeck
	g.SetZone(ZPlanarDeck, 0, []ObjID{a.ID})
	g.SetZone(ZPlanarDeck, 1, []ObjID{b.ID})
	if !ZPlanarDeck.Valid() || !ZPlanarDeck.Hidden() || ZPlanarDeck.String() != "planar_deck" {
		t.Fatalf("planar zone contract: valid=%v hidden=%v name=%q", ZPlanarDeck.Valid(), ZPlanarDeck.Hidden(), ZPlanarDeck.String())
	}
	if len(g.Zone(ZPlanarDeck, 0)) != 1 || g.Zone(ZPlanarDeck, 0)[0] != a.ID || len(g.Zone(ZPlanarDeck, 1)) != 1 || g.Zone(ZPlanarDeck, 1)[0] != b.ID {
		t.Fatalf("seat planar zones overlap: A=%v B=%v", g.Zone(ZPlanarDeck, 0), g.Zone(ZPlanarDeck, 1))
	}
	clone := g.Clone()
	clone.SetZone(ZPlanarDeck, 0, nil)
	if len(clone.Zone(ZPlanarDeck, 0)) != 0 || len(g.Zone(ZPlanarDeck, 0)) != 1 {
		t.Fatal("clone planar zone shares its backing state")
	}
}
