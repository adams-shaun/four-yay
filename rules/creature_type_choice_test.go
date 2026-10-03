package rules

import (
	"testing"
)

// TestCreatureTypeChoiceOffersEveryType pins CR 205.3m on the shared
// creature-type option list: it leads with the chooser's own types (so the
// deterministic first option is unchanged) and then offers every other
// creature type, including ones no card in the game has.
func TestCreatureTypeChoiceOffersEveryType(t *testing.T) {
	t.Parallel()
	e, _ := pcdrEngine(t, "Savannah Lions")
	opts := e.creatureTypeOptions(0)
	if len(opts) == 0 || opts[0].Label != "Cat" {
		t.Fatalf("first option = %v, want the chooser's own Cat first", opts[:min(3, len(opts))])
	}
	seen := map[string]int{}
	for i, o := range opts {
		if o.Index != i {
			t.Fatalf("option %d carries index %d", i, o.Index)
		}
		seen[o.Label]++
	}
	for _, want := range []string{"Bear", "Sliver", "Zombie", "Human"} {
		if seen[want] != 1 {
			t.Errorf("type %q offered %d times, want exactly once", want, seen[want])
		}
	}
	if seen["Cat"] != 1 {
		t.Errorf("owned type Cat offered %d times, want exactly once", seen["Cat"])
	}
}
