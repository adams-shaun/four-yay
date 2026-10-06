package templates

import (
	"testing"

	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// Parameter-driven scenarios must establish their reduction precondition and
// replay through gorge; the exact reduced price must fail without the static.
func TestCostStaticParameterFixturesReplayAndRequireStatic(t *testing.T) {
	reg := loadGenRegistry(t)
	for _, name := range []string{"Eddymurk Crab", "Polliwallop", "Dire Downdraft"} {
		t.Run(name, func(t *testing.T) {
			it := staticCostItem(t, reg, name)
			if _, ok := oraclegen.PlaysThrough(reg, it.Scenario); !ok {
				t.Fatalf("precondition: parameter probe does not play through: %+v", it.Steps)
			}
			if name == "Eddymurk Crab" {
				gy := it.Scenario.Setup["p0"].Graveyard
				if len(gy) == 0 {
					t.Fatal("precondition: Eddymurk Crab has no instant/sorcery graveyard fixture")
				}
			}
			if res := runSteps(t, withoutStatics(reg, name), it.Scenario, it.Steps); len(res.Fails) == 0 {
				t.Fatalf("cast at reduced price succeeded with %s's static removed", name)
			}
		})
	}
}
