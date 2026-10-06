package oraclegen_test

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// Shared costs must be extended in XMage's {this} text space, including
// when the first distinguishing word comes AFTER a self-reference.
func TestXMageAbilitySharedCostSelfReference(t *testing.T) {
	for _, separator := range []string{"\n", `\n`} {
		t.Run(separator, func(t *testing.T) {
			f := &cards.Face{
				Name:   "Test Source",
				Oracle: "{T}: Test Source gains flying." + separator + "{T}: Test Source gains vigilance.",
				Abilities: []*cards.SA{
					{Kind: "AB"}, {Kind: "AB"},
				},
			}
			if !f.Abilities[0].IsActivated() || !f.Abilities[1].IsActivated() {
				t.Fatal("precondition: both slots must be activated abilities")
			}
			got, why := oraclegen.XMageAbility(f)
			if why != "" || len(got) != 2 {
				t.Fatalf("shared-cost mapping = %v, %q; want two unique prefixes", got, why)
			}
			full := []string{"{T}: {this} gains flying.", "{T}: {this} gains vigilance."}
			for i, want := range full {
				if got[i] != want {
					t.Errorf("ability %d = %q, want %q", i, got[i], want)
				}
				for j, line := range full {
					if strings.HasPrefix(line, got[i]) != (i == j) {
						t.Errorf("prefix %q selects line %d (%q), want only %d", got[i], j, line, i)
					}
				}
			}

			// A control with distinguishable text above proves the mapping
			// handler ran; identical rules cannot be selected uniquely.
			f.Oracle = "{T}: Test Source gains flying." + separator + "{T}: Test Source gains flying."
			got, why = oraclegen.XMageAbility(f)
			if got != nil || why != "activate xmage text ambiguous" {
				t.Fatalf("identical rules = %v, %q; want ambiguous", got, why)
			}
		})
	}
}

// A short self-reference cannot be copied into the longer prefix: XMage
// prints {this} there, and the mapper deliberately fails closed.
func TestXMageAbilitySharedCostShortNameFailsClosed(t *testing.T) {
	f := &cards.Face{
		Name:   "Test Source, the Example",
		Oracle: "{T}: Draw a card.\n{T}: Gain 1 life.",
		Abilities: []*cards.SA{
			{Kind: "AB"}, {Kind: "AB"},
		},
	}
	got, why := oraclegen.XMageAbility(f)
	if why != "" || got[0] != "{T}: Draw" || got[1] != "{T}: Gain" {
		t.Fatalf("precondition: distinguishable control = %v, %q", got, why)
	}
	f.Oracle = "{T}: Test Source gains flying.\n{T}: Test Source gains vigilance."
	got, why = oraclegen.XMageAbility(f)
	if got != nil || why != "activate xmage text ambiguous" {
		t.Fatalf("short-name extension = %v, %q; want ambiguous", got, why)
	}
}
