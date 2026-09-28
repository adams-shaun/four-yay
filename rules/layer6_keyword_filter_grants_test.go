package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

// TestWithKeywordPredicateSeesLayer6GrantAddAbilities proves a live
// AddAbilities Affects filter consumes the same derived keyword view as the
// restriction paths. The recipient does not print Flying: it receives it
// only from the earlier layer-6 effect, and expiry withdraws both the keyword
// and the granted ability.
func TestWithKeywordPredicateSeesLayer6GrantAddAbilities(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	grantor := onBoard(t, e, 0, "Name:Grantor\nManaCost:1 U\nTypes:Creature Wizard\nPT:1/1\nSVar:ABGranted:AB$ Untap | Cost$ 0 | Defined$ Self\nOracle:x\n")
	bear := onBoard(t, e, 0, "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	for _, id := range []state.ObjID{grantor, bear} {
		o := e.G.Obj(id)
		if o == nil || o.Zone != state.ZBattlefield || o.Face() == nil {
			t.Fatalf("object %d precondition: %+v", id, o)
		}
	}
	if e.G.Obj(bear).Face().HasKeyword("Flying") {
		t.Fatal("bear must not print Flying")
	}

	e.AddContinuous(ContinuousEffect{Source: grantor, Controller: 0, Timestamp: 1,
		Layer: LAbilities, Affects: "Creature.YouCtrl+withFlying", AddAbilities: []string{"ABGranted"}, UntilEOT: true})
	if got := e.grantedAbilities(0, bear); len(got) != 0 {
		t.Fatalf("ungranted bear received %d ability grants", len(got))
	}

	e.AddContinuous(ContinuousEffect{Source: grantor, Controller: 0, Timestamp: 2,
		Layer: LAbilities, Affects: "Creature.YouCtrl", AddKeywords: []string{"Flying"}, UntilEOT: true})
	if !e.HasKeyword(bear, "Flying") {
		t.Fatal("layer-6 Flying grant is not derived")
	}
	got := e.grantedAbilities(0, bear)
	if len(got) != 1 || got[0].svar != "ABGranted" || got[0].sa == nil {
		t.Fatalf("derived Flying recipient grants = %+v, want ABGranted", got)
	}

	e.EndOfTurnCleanup()
	if e.HasKeyword(bear, "Flying") {
		t.Fatal("expired Flying grant remains derived")
	}
	if got := e.grantedAbilities(0, bear); len(got) != 0 {
		t.Fatalf("expired Flying recipient retained %d ability grants", len(got))
	}
}
