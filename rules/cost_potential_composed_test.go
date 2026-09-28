package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

// Two independent target-relative discounts must apply to the SAME
// announcement; the best of each static cannot be summed across candidates.
func TestPotentialCostComposesReductionsForOneCandidate(t *testing.T) {
	t.Parallel()
	spellCard := card(t, "Name:Test Bolt\nManaCost:2\nTypes:Instant\nA:SP$ DealDamage | ValidTgts$ Creature | NumDmg$ 1\nOracle:x\n")
	e := handEngine(t, spellCard)
	source := func(name string) state.ObjID {
		return battlePerm(t, e, 0, "Name:"+name+"\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nS:Mode$ ReduceCost | ValidTarget$ Card.Self | Activator$ You | Type$ Spell | Amount$ 1 | Description$ Discount a spell targeting CARDNAME.\nOracle:x\n")
	}
	a, b := source("First Bear"), source("Second Bear")
	spell := e.G.Zone(state.ZHand, 0)[0]
	base := e.parseCost("2")
	cands := e.costPotentialTargets(0, spell, spellScope(""))
	if base.Generic != 2 || len(cands) != 2 || cands[0].Obj != a || cands[1].Obj != b ||
		e.G.Obj(spell).Zone != state.ZHand || e.G.Obj(a).Zone != state.ZBattlefield || e.G.Obj(b).Zone != state.ZBattlefield {
		t.Fatalf("precondition: base=%+v candidates=%+v zones=%s,%s,%s", base, cands, e.G.Obj(spell).Zone, e.G.Obj(a).Zone, e.G.Obj(b).Zone)
	}
	statics := e.collectCostStatics()
	if len(statics.reduce) != 2 {
		t.Fatalf("precondition: expected both reducers, got %d", len(statics.reduce))
	}
	for _, cand := range cands {
		mods := e.costModifiersWithTargetsUsing(statics, 0, spell, spellScope(""), []state.Target{cand}, true)
		if got := mods.apply(base).Generic; got != 1 || len(mods.reduces) != 1 {
			t.Fatalf("precondition: target %d earns exactly ONE reduction, cost=%d reductions=%d", cand.Obj, got, len(mods.reduces))
		}
	}
	// Neither candidate pays {0}; an offer at an empty pool would strand the
	// cast at targetAsk, where both candidates are rejected.
	if e.offerCastable(0, spell, base, spellScope(""), false) {
		t.Fatal("offer gate combined discounts from different candidate targets")
	}
	_, ok := e.potentialCostModsUsing(statics, 0, spell, spellScope(""), cands, 0, func(m costMods) bool {
		return m.apply(base).Generic == 0
	})
	if ok {
		t.Fatal("candidate composition admitted a free price no target can earn")
	}
}
