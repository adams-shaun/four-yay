package templates

import (
	"testing"

	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// fillActivateXAbilityCensusCards are one served level-B static row per
// measured empty-XAbility hole (agent 20261009T041408Z, cluster C5): the
// Surveillance CanAttackDefender branch, the UntapOtherPlayer mana-ability
// tap, and the gated CantBlockBy prelude's probe sacrifice. XMage's replay
// driver throws "activate step N has no xmage_ability" on any served activate
// step whose XAbility slot is empty, so every served row carrying an activate
// step must name it.
var fillActivateXAbilityCensusCards = []string{
	"Surveillance Phantasm",
	"Bender's Waterskin",
	"Furtive Courier",
}

// TestServedRowsFillActivateXAbility regenerates the cards' served level-B
// rows through GenerateB (the one funnel every serving site returns through)
// and asserts the served items name every activate step they carry. A card
// whose served rows carry NO activate step fails the precondition instead of
// passing vacuously.
func TestServedRowsFillActivateXAbility(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, name := range fillActivateXAbilityCensusCards {
		name := name
		t.Run(name, func(t *testing.T) {
			c, ok := reg.Lookup(name)
			if !ok {
				t.Fatalf("%s is not in the corpus", name)
			}
			activated := false
			for _, req := range levelb.Requirements(c) {
				it, skip := GenerateB(reg, name, req)
				if skip != nil {
					continue
				}
				if len(it.XAbility) != 0 && len(it.XAbility) != len(it.Steps) {
					t.Fatalf("%s %s: XAbility %d entries for %d steps",
						name, req.Key, len(it.XAbility), len(it.Steps))
				}
				for i, st := range it.Steps {
					if st.Op != "activate" {
						continue
					}
					activated = true
					if i >= len(it.XAbility) || it.XAbility[i] == "" {
						t.Errorf("%s %s: activate step %d (card %s, ability index %v) has an empty XAbility slot",
							name, req.Key, i, st.Card, st.AbilityIndex)
					}
				}
			}
			if !activated {
				t.Fatalf("%s served no level-B row with an activate step: the fixture is vacuous", name)
			}
		})
	}
}
