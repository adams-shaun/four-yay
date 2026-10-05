package effects

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestRepeatTypesFromFindsDistinctTypesOnImprintedLibraryCards pins the
// RepeatTypesFrom source population: repeated card types are visited once,
// and only cards matching the source's IsImprinted association contribute.
func TestRepeatTypesFromFindsDistinctTypesOnImprintedLibraryCards(t *testing.T) {
	h, src, lib := riderBoard(t,
		"Name:Bear\nTypes:Artifact Creature Bear\nPT:2/2\nOracle:x\n",
		riderLand)
	h.Emit(events.Event{Kind: events.Imprint, Obj: src, IDs: append([]state.ObjID(nil), lib...), Text: "seek-found"})
	got, ok := repeatEachTypesFrom(h, &Ctx{Source: src, Controller: 0}, "ValidLibrary Card.IsImprinted")
	if !ok {
		t.Fatal("RepeatTypesFrom selector unexpectedly unsupported")
	}
	want := []string{"Artifact", "Creature", "Land"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("RepeatTypesFrom types = %v, want %v", got, want)
	}
}
