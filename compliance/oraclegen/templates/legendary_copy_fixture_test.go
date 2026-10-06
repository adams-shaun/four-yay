package templates_test

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/compliance/oraclegen/templates"
)

// TestCopyTargetFixtureIsNotLegendary: Sygg's Command's "create a token
// that's a copy of target Merfolk you control" once drew Ambassador Laquatus,
// a legendary Merfolk. The copy puts two Laquatuses under the legend rule and
// the scripted keep answer names both, so XMage kept the original or the
// token at random (2 of 4 replays put the card in the graveyard). The quiet
// subtype fixture now skips legendary creatures, so no setup permanent the
// scenario places is legendary.
func TestCopyTargetFixtureIsNotLegendary(t *testing.T) {
	reg := corpusReg(t)
	it, skip := templates.Generate(reg, "Sygg's Command")
	if skip != nil {
		t.Fatalf("Sygg's Command: %s", skip.Reason)
	}
	for seat, s := range it.Setup {
		for _, name := range s.Battlefield {
			c, ok := reg.Lookup(name)
			if !ok || len(c.Faces) == 0 {
				t.Fatalf("%s battlefield card %q not in the corpus", seat, name)
			}
			for _, typ := range c.Faces[0].Types {
				if strings.EqualFold(typ, "Legendary") {
					t.Errorf("%s battlefield fixture %q is legendary; a copy of it is a legend-rule coin flip in XMage", seat, name)
				}
			}
		}
	}
}
