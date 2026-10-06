package levelb

import (
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

// A static whose only effect is a Forge AI hint (AddSVar$ of HasAttackEffect,
// DestroyWhenDamaged, ...) gives no level-B requirement; its card's other
// statics keep theirs.
func TestAIHintOnlyStaticsHaveNoRequirement(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, name := range []string{"Ordeal of Nylea", "Cracked Skull"} {
		c, ok := reg.Lookup(name)
		if !ok {
			t.Fatalf("%s not in the corpus", name)
		}
		if !AIHintOnlyStatic(c.Faces[0], &c.Faces[0].Statics[0]) {
			t.Errorf("%s static 0 is not classified AI-hint-only", name)
		}
		for _, r := range Requirements(c) {
			if r.Key == "static#0.0" {
				t.Errorf("%s still has requirement static#0.0 (%s)", name, r.Sub)
			}
		}
	}
	// Vizier's look-at static adds no SVar: it keeps its requirement.
	c, _ := reg.Lookup("Vizier of the Menagerie")
	if AIHintOnlyStatic(c.Faces[0], &c.Faces[0].Statics[0]) {
		t.Error("Vizier's MayLookAt static classified AI-hint-only")
	}
}
