package effects

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func TestRepeatTypesFromExcludesSubtypesAsCardTypes(t *testing.T) {
	h, src, lib := riderBoard(t,
		"Name:VehicleCard\nTypes:Artifact Vehicle\nOracle:x\n",
		"Name:RoomCard\nTypes:Enchantment Room\nOracle:x\n",
		"Name:SpacecraftCard\nTypes:Creature Spacecraft\nOracle:x\n",
	)
	if len(lib) != 3 {
		t.Fatalf("fixture has %d library cards, want 3", len(lib))
	}
	h.Emit(events.Event{Kind: events.Imprint, Obj: src, IDs: append([]state.ObjID(nil), lib...), Text: "seek-found"})
	got, ok := repeatEachTypesFrom(h, &Ctx{Source: src, Controller: 0}, "ValidLibrary Card.IsImprinted")
	if !ok {
		t.Fatal("RepeatTypesFrom selector unexpectedly unsupported")
	}
	want := []string{"Artifact", "Creature", "Enchantment"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("RepeatTypesFrom types = %v, want %v (Vehicle, Room, and Spacecraft are subtypes)", got, want)
	}
}
