package templates

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// The counter template casts a precast spell, then targets it and a second
// object during the counter's later ability. The first slot must not consume
// a skip meant for the later slot (or vice versa).
func TestCounterSpellEmitsPerSlotTargetGroups(t *testing.T) {
	reg := loadGenRegistry(t)
	f := faceOf(t, reg, "Sokka's Haiku")
	slots := oraclegen.SlotInfos(f)
	if len(slots) != 2 || slots[1].Filter() != "Land" {
		t.Fatalf("precondition: Sokka's Haiku must have counter and land slots, got %+v", slots)
	}
	it, skip := Generate(reg, "Sokka's Haiku")
	if skip != nil {
		t.Fatalf("Sokka's Haiku skipped: %s", skip.Reason)
	}
	if it.Template != CounterSpell.ID || len(it.Steps) < 2 || it.Steps[0].Op != "cast" || it.Steps[1].Op != "cast" {
		t.Fatalf("precondition: want precast and counter cast, got %s %+v", it.Template, it.Steps)
	}
	pre, counter := it.Steps[0], it.Steps[1]
	if len(counter.Targets) != 2 || counter.Targets[0] != pre.Card || counter.Targets[1] == counter.Targets[0] {
		t.Fatalf("precondition: counter must target precast and distinct land: pre=%+v counter=%+v", pre, counter)
	}
	if len(counter.TargetGroups) != 2 {
		t.Fatalf("counter groups = %+v, want one per slot", counter.TargetGroups)
	}
	for i, want := range counter.Targets {
		if !reflect.DeepEqual(counter.TargetGroups[i].Picks, []string{want}) || counter.TargetGroups[i].Max != slots[i].Max() {
			t.Errorf("group %d = %+v, want target %q max %d", i, counter.TargetGroups[i], want, slots[i].Max())
		}
	}
	wire, err := json.Marshal(counter)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(wire, &decoded); err != nil {
		t.Fatal(err)
	}
	if _, ok := decoded["target_groups"]; !ok {
		t.Fatalf("counter cast wire omits target_groups: %s", wire)
	}
}

func TestSingleSlotCounterCarriesNoGroups(t *testing.T) {
	reg := loadGenRegistry(t)
	it, skip := Generate(reg, "Counterspell")
	if skip != nil {
		t.Fatalf("Counterspell skipped: %s", skip.Reason)
	}
	if it.Template != CounterSpell.ID || len(it.Steps) < 2 || len(it.Steps[1].Targets) != 1 {
		t.Fatalf("precondition: want one-slot counter cast, got %s %+v", it.Template, it.Steps)
	}
	if len(it.Steps[1].TargetGroups) != 0 {
		t.Errorf("single-slot counter carries groups: %+v", it.Steps[1].TargetGroups)
	}
}
