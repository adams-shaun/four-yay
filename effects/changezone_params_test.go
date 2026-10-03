package effects

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// TestChangeZoneKnownKeysSorted: the unread lookup binary-searches the table.
func TestChangeZoneKnownKeysSorted(t *testing.T) {
	if !slices.IsSorted(changeZoneKnownKeys[:]) || len(slices.Compact(slices.Clone(changeZoneKnownKeys[:]))) != len(changeZoneKnownKeys) {
		t.Fatal("changeZoneKnownKeys must be sorted and duplicate-free")
	}
}

// TestCompileChangeZone pins the compiled shapes a sibling path reads: the
// merged origin set and its mask, Origin$ alone, the destination zone, the
// MANDATORY alternate-destination prefix, the hand chooser enum, and the
// unread report.
func TestCompileChangeZone(t *testing.T) {
	sa := &cards.SA{API: "ChangeZone", Params: map[string]string{
		"Origin": "Graveyard", "OriginAlternative": "Hand,Exile", "Destination": "Battlefield",
		"DestAltSVar": "MANDATORY X", "DestinationAlternative": "Exile", "Chooser": "Player.Chosen",
		"Hidden": "True", "Optional": "You", "ValidTgtsDesc": "a creature card",
	}}
	cz := ChangeZoneOf(sa)
	if !slices.Equal(cz.Origin, []state.Zone{state.ZGraveyard, state.ZHand, state.ZExile}) ||
		!cz.OriginMask.Has(state.ZHand) || cz.OriginMask.Has(state.ZLibrary) {
		t.Fatalf("origin = %v mask %b", cz.Origin, cz.OriginMask)
	}
	if !cz.OriginExactly(state.ZGraveyard) || !cz.DestinationIs(state.ZBattlefield) {
		t.Fatal("Origin$ alone / Destination$ misread")
	}
	if !cz.DestAltMandatory || cz.DestAltCond != "X" || cz.DestinationAlt != state.ZExile {
		t.Fatalf("alt destination = %+v", cz)
	}
	if cz.handChooser != handChooserChosenPlayer || !cz.Hidden || !cz.OptionalYes || cz.OptionalTrue {
		t.Fatalf("chooser/flags = %+v", cz)
	}
	if !slices.Equal(cz.Unread, []string{"ValidTgtsDesc"}) {
		t.Fatalf("unread = %v, want [ValidTgtsDesc]", cz.Unread)
	}
	// The front cache answers the same map with the same record; a
	// rewritten map recompiles.
	if ChangeZoneOf(sa) != cz {
		t.Fatal("front cache missed the same Params map")
	}
	copyWithNewParams := *sa
	copyWithNewParams.Params = map[string]string{"Destination": "Hand"}
	if got := ChangeZoneOf(&copyWithNewParams); got == cz || !got.DestinationIs(state.ZHand) {
		t.Fatal("a rewritten Params map read the stale record")
	}
}
