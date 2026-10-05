package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

// A retained card type keeps its own subtype when impending suppresses
// Creature and its creature subtypes. The positive creature-type vocabulary
// must not erase artifact or enchantment subtypes.
func TestImpendingRetainsNoncreatureSubtype(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, printed, retained, subtype string }{
		{"equipment", "Artifact Creature Equipment Golem", "Artifact", "Equipment"},
		{"saga", "Enchantment Creature Saga Golem", "Enchantment", "Saga"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := "Name:Impending Test Golem\nManaCost:3 G\nTypes:" + tc.printed + "\nPT:4/4\n" +
				"K:Impending:2:1 G\nOracle:Impending 2—{1}{G}\n"
			e, cfg, _ := altCostEngine(t, 9608, nil, []string{src}, nil)
			id := castImpending(t, e)
			o := e.G.Obj(id)
			if o == nil || o.Zone != state.ZBattlefield || o.CastFlags&state.FlagImpending == 0 || o.Counter("TIME") != 2 {
				t.Fatalf("precondition: impending permanent must enter dormant on battlefield: %+v", o)
			}
			if got := o.Face().Types; !impendingContainsWord(got, tc.subtype) || !impendingContainsWord(got, "Golem") {
				t.Fatalf("precondition: printed types %v do not contain both subtype families", got)
			}
			types := e.Derived(id).Types
			if !impendingContainsWord(types, tc.retained) || !impendingContainsWord(types, tc.subtype) ||
				impendingContainsWord(types, "Creature") || impendingContainsWord(types, "Golem") {
				t.Errorf("dormant derived types = %v, want %s %s only", types, tc.retained, tc.subtype)
			}
			replayCheck(t, e, cfg)
		})
	}
}
