package templates

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// affinityFixtures places a permanent of the named type; every face it can
// return must be a name XMage's database holds, not only the card's front face.
func TestAffinityFixturesSkipUnknownFaces(t *testing.T) {
	oraclegen.SetXMageKnown([]string{"Delver of Secrets", "Grizzly Bears"})
	t.Cleanup(func() { oraclegen.SetXMageKnown(nil) })
	for n, want := range map[string]bool{
		"Delver of Secrets": true, "Grizzly Bears": true,
		"Insectile Aberration": false, "Adorable Kitten": false,
	} {
		if oraclegen.XMageKnown(n) != want {
			t.Fatalf("precondition: XMageKnown(%q) = %v, want %v", n, !want, want)
		}
	}
	reg := cards.NewRegistry()
	// Known front face, unknown Insect back face.
	reg.Add(&cards.Card{Faces: []*cards.Face{
		{Name: "Delver of Secrets", Types: []string{"Creature", "Human", "Wizard"}},
		{Name: "Insectile Aberration", Types: []string{"Creature", "Human", "Insect"}},
	}})
	// Entirely unknown card.
	reg.Add(&cards.Card{Faces: []*cards.Face{
		{Name: "Adorable Kitten", Types: []string{"Creature", "Cat", "Insect"}},
	}})
	reg.Add(&cards.Card{Faces: []*cards.Face{
		{Name: "Grizzly Bears", Types: []string{"Creature", "Bear", "Insect"}},
	}})
	got := affinityFixtures(reg, "Insect", 3)
	if len(got) != 1 || got[0] != "Grizzly Bears" {
		t.Fatalf("affinityFixtures = %v, want [Grizzly Bears]", got)
	}
}
