package effects

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// TestChangeZoneAllKnownKeysSorted: the unread lookup binary-searches the
// table.
func TestChangeZoneAllKnownKeysSorted(t *testing.T) {
	if !slices.IsSorted(changeZoneAllKnownKeys[:]) || len(slices.Compact(slices.Clone(changeZoneAllKnownKeys[:]))) != len(changeZoneAllKnownKeys) {
		t.Fatal("changeZoneAllKnownKeys must be sorted and duplicate-free")
	}
}

// TestCompileChangeZoneAll pins the compiled shapes: the Any/All origin
// expansion, the destination and its removal word, the ChangeNum$ cap rule,
// the player-scope selectors, the shared riders and the unread report.
func TestCompileChangeZoneAll(t *testing.T) {
	sa := &cards.SA{API: "ChangeZoneAll", Params: map[string]string{
		"Origin": "Any", "Destination": "Exile", "ChangeNum": "All", "ValidTgts": "Player",
		"RandomOrder": "True", "ForgetOtherRemembered": "True", "Duration": "UntilHostLeavesPlay",
		"Hidden": "True",
	}}
	p := ChangeZoneAllOf(sa)
	if !p.OriginOK || !slices.Equal(p.Origin, changeZoneAllEveryZone) {
		t.Fatalf("origin = %v ok %v", p.Origin, p.OriginOK)
	}
	if p.Destination != state.ZExile || p.DestinationLower != "exile" || p.ChangeType != "Card" {
		t.Fatalf("destination/type = %+v", p)
	}
	if p.ChangeNumCapped || !p.Targeting || p.ValidTgtsText != "Player" || p.DefinedPresent {
		t.Fatalf("cap/scope = %+v", p)
	}
	if !p.RandomOrder || !p.Riders.ForgetOtherRemembered || p.Riders.Duration.Text != "UntilHostLeavesPlay" {
		t.Fatalf("flags/riders = %+v", p)
	}
	if !slices.Equal(p.Unread, []string{"Hidden"}) {
		t.Fatalf("unread = %v, want [Hidden]", p.Unread)
	}
	if ChangeZoneAllOf(sa) != p {
		t.Fatal("front cache missed the same Params map")
	}
	rewritten := *sa
	rewritten.Params = map[string]string{"Origin": "Graveyard", "Destination": "Library", "ChangeNum": "2"}
	q := ChangeZoneAllOf(&rewritten)
	if q == p || !q.ChangeNumCapped || !slices.Equal(q.Origin, []state.Zone{state.ZGraveyard}) || q.Destination != state.ZLibrary {
		t.Fatalf("a rewritten Params map read the stale record: %+v", q)
	}
}
