package rules

import (
	"sort"
	"testing"

	"github.com/adams-shaun/gorge/state"
)

// TestETBCreatureTypeChoiceHonoursValidTypes drives the real Dawn-Blessed
// Pennant carrier ("As this artifact enters, choose Elemental, Elf, Faerie,
// Giant, Goblin, Kithkin, Merfolk, or Treefolk") through the entry boundary:
// its Type$ Creature ask carries ValidTypes$ eight types, so the offered list
// must be exactly those eight, never the whole 362-type creature vocabulary.
func TestETBCreatureTypeChoiceHonoursValidTypes(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusAlternativeCard(t, "Dawn-Blessed Pennant"))
	id := e.G.Zone(state.ZHand, 0)[0]
	labels := etbTypeChoiceLabels(t, e, id)
	want := []string{"Elemental", "Elf", "Faerie", "Giant", "Goblin", "Kithkin", "Merfolk", "Treefolk"}
	if len(labels) != len(want) {
		t.Fatalf("Type$ Creature + ValidTypes$ options = %v, want exactly %v", labels, want)
	}
	got := append([]string(nil), labels...)
	sort.Strings(got)
	sortedWant := append([]string(nil), want...)
	sort.Strings(sortedWant)
	for i := range sortedWant {
		if got[i] != sortedWant[i] {
			t.Fatalf("Type$ Creature + ValidTypes$ options = %v, want %v", labels, want)
		}
	}
	if containsLabel(labels, "Human") {
		t.Fatalf("ValidTypes$ leaked an unlisted creature type: %v", labels)
	}
}

// TestTypeChoicesHonoursCreatureFilters pins the Engine.TypeChoices contract
// directly: an empty filter pair keeps the whole creature vocabulary, while
// ValidTypes$ narrows and InvalidTypes$ removes.
func TestTypeChoicesHonoursCreatureFilters(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusAlternativeCard(t, "Dawn-Blessed Pennant"))
	all := e.TypeChoices(0, "Creature", "", "")
	if len(all) < 100 {
		t.Fatalf("precondition: unfiltered creature vocabulary = %d options, want the full list", len(all))
	}
	filtered := e.TypeChoices(0, "Creature", "Elf,Goblin", "")
	var labels []string
	for _, o := range filtered {
		labels = append(labels, o.Label)
	}
	if len(labels) != 2 || !containsLabel(labels, "Elf") || !containsLabel(labels, "Goblin") {
		t.Fatalf("TypeChoices(ValidTypes$ Elf,Goblin) = %v, want exactly [Elf Goblin]", labels)
	}
	invalid := e.TypeChoices(0, "Creature", "", "Elf,Goblin")
	for _, o := range invalid {
		if o.Label == "Elf" || o.Label == "Goblin" {
			t.Fatalf("InvalidTypes$ Elf,Goblin still offered %q", o.Label)
		}
	}
}
