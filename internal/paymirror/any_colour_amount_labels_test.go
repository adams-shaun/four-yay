package paymirror

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
)

func TestMatchProductionPrefersAnyColourAmountOption(t *testing.T) {
	d := &decision.Decision{Options: []decision.Option{
		{Index: 0, Kind: "mana", Ability: 0, Label: "Add any color"},
		{Index: 1, Kind: "mana", Ability: 1, Label: "Add three mana of any one color"},
	}}
	want := decision.ManaAmount{0, 0, 0, 0, 3, 0}
	got := matchProduction(d, want, 1)
	if got != 1 {
		t.Fatalf("matchProduction(want 3G, prefer ability 1) = %d, want option 1", got)
	}

	// The Combo Any label emitted by the same rules formatter is parsed as an
	// any-colour production too, not rejected as an unknown label.
	_, any, _, ok := labelProduction("Add three mana in any combination of colors")
	if !ok || !any {
		t.Fatalf("Combo Any amount label parsed as any=%v ok=%v, want true/true", any, ok)
	}
}
