package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

// TestRoomAltCantBeCastFrontRestricted ensures a restriction matching only a
// Room's displayed front door does not withhold its legal alternate door.
func TestRoomAltCantBeCastFrontRestricted(t *testing.T) {
	reg := searchTestRegistry(t)
	e, cfg := searchEngine(t, reg, "Experimental Lab", "Gaddock Teeg")
	id := searchMoveByName(t, e, "Experimental Lab", state.ZHand)
	moveRestrictionSource(t, e, "Gaddock Teeg")
	o := e.G.Obj(id)
	if o.Zone != state.ZHand || int(o.FaceIdx) != 0 {
		t.Fatalf("precondition: Experimental Lab zone=%s faceIdx=%d, want hand/front", o.Zone, o.FaceIdx)
	}
	rf := roomAlternateCastFace(o)
	if rf == nil || rf.Name != "Staff Room" {
		t.Fatalf("precondition: Room alternate face is %v, want Staff Room", rf)
	}
	if !e.castRestricted(0, id) {
		t.Fatal("precondition: Experimental Lab front is not restricted by Gaddock Teeg")
	}
	if probeFaceRestricted(t, e, id, rf) {
		t.Fatal("precondition: Staff Room alternate door is restricted too")
	}

	// Cover both costs so affordability cannot mask either option assertion.
	addMana(t, e, 0, "GGGG")
	if alt := altFaceOption(e, id, "room_alt"); alt == nil || alt.Label != "Cast Staff Room" {
		t.Fatalf("room_alt withheld although only the front door is restricted: %+v", castOptions(t, e))
	}
	if plain := altFaceOption(e, id, ""); plain != nil {
		t.Fatalf("front cast offered although Experimental Lab is restricted: %+v", plain)
	}
	replayCheck(t, e, cfg)
}

// TestRoomAltCantBeCastBackRestricted ensures a restriction matching only the
// alternate Room door withholds that offer while leaving the front cast legal.
func TestRoomAltCantBeCastBackRestricted(t *testing.T) {
	reg := searchTestRegistry(t)
	e, cfg := searchEngine(t, reg, "Mirror Room", "Gaddock Teeg")
	id := searchMoveByName(t, e, "Mirror Room", state.ZHand)
	moveRestrictionSource(t, e, "Gaddock Teeg")
	o := e.G.Obj(id)
	if o.Zone != state.ZHand || int(o.FaceIdx) != 0 {
		t.Fatalf("precondition: Mirror Room zone=%s faceIdx=%d, want hand/front", o.Zone, o.FaceIdx)
	}
	rf := roomAlternateCastFace(o)
	if rf == nil || rf.Name != "Fractured Realm" {
		t.Fatalf("precondition: Room alternate face is %v, want Fractured Realm", rf)
	}
	if e.castRestricted(0, id) {
		t.Fatal("precondition: Mirror Room front is restricted, want unrestricted")
	}
	if !probeFaceRestricted(t, e, id, rf) {
		t.Fatal("precondition: Fractured Realm alternate door is not restricted")
	}

	// Fund both doors, especially the seven-mana alternate, so its absence is
	// attributable only to CantBeCast.
	addMana(t, e, 0, "UUUUUUU")
	if alt := altFaceOption(e, id, "room_alt"); alt != nil {
		t.Fatalf("room_alt offered for the prohibited Fractured Realm door: %+v", alt)
	}
	if plain := altFaceOption(e, id, ""); plain == nil || plain.Label != "Cast Mirror Room" {
		t.Fatalf("unrestricted Mirror Room front cast withheld: %+v", castOptions(t, e))
	}
	replayCheck(t, e, cfg)
}
