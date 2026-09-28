package rules

// fb-20260923T005805Z-1301f55a: the cast-offer affordability gate for a cost
// whose announced Sac<X/Spec> count drives a ReduceCost static. Dargo, the
// Shipwrecker is the named corpus carrier (SVar:X:Count$xPaid over
// SVar:Y:SVar$X/Times.2). Before the fix the ordinary cast option was priced
// with the pre-announcement modifier snapshot (X=0), so a player who could
// only afford the cast AFTER sacrificing got no option at all.

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// TestDargoCastOfferedWhenSacrificeReducesManaCost is the reported symptom:
// three eligible permanents and a pool of {1}{R} that cannot pay the
// unreduced {6}{R}, but can pay the {R} left after announcing X=3 (three
// permanents sacrificed, {2} less each). The cast option must appear, the
// announcement/payment must succeed, and all three permanents must be
// sacrificed.
func TestDargoCastOfferedWhenSacrificeReducesManaCost(t *testing.T) {
	t.Parallel()
	e, spell, ids := dargoEngine(t, []string{
		"Name:Anvil\nTypes:Artifact\nOracle:x\n",
		"Name:Crab\nTypes:Creature\nPT:1/1\nOracle:x\n",
		"Name:Boar\nTypes:Creature\nPT:2/2\nOracle:x\n",
	}, "1R")
	// Precondition: the eligible permanents really are on the battlefield and
	// the pool really is below the unreduced {6}{R} = 7 total. A vacuous setup
	// (an empty battlefield, or a pool that already pays full price) must fail
	// loudly here rather than pass the offer assertion silently.
	onField := 0
	for _, id := range ids {
		if e.G.Obj(id).Zone == state.ZBattlefield {
			onField++
		}
	}
	if onField != 3 {
		t.Fatalf("precondition: %d of 3 eligible permanents on the battlefield, want 3", onField)
	}
	if pool := e.G.Players[0].Pool; pool.Total() >= 7 {
		t.Fatalf("precondition: pool %+v totals %d, want below the unreduced {6}{R}=7", pool, pool.Total())
	}
	// The pool can still pay the reduced {R} after all three sacrifices.
	idx := castOption(t, e, spell)
	submitChoices(t, e, idx)
	dx := e.Pending()
	if dx == nil || dx.Kind != decision.KChoose || len(dx.Options) == 0 || dx.Options[0].Kind != "x" {
		t.Fatalf("Sac<X> did not announce a count ask: %+v", dx)
	}
	// The reducing announcement (X=3) must be among the offered values; the
	// pre-fix xAsk broke at the first unpayable X and offered none.
	sawThree := false
	threeIdx := -1
	for _, o := range dx.Options {
		if o.Kind == "x" && o.Amount == 3 {
			sawThree = true
			threeIdx = o.Index
		}
	}
	if !sawThree {
		t.Fatalf("X=3 (the reducing announcement) not offered: %+v", dx.Options)
	}
	submitChoices(t, e, threeIdx)
	ds := e.Pending()
	if ds == nil || ds.Kind != decision.KChoose || ds.Min != 3 || ds.Max != 3 {
		t.Fatalf("sacrifice ask %+v, want Min/Max 3", ds)
	}
	chosen := make([]int, 0, 3)
	for _, o := range ds.Options {
		chosen = append(chosen, o.Index)
	}
	if len(chosen) != 3 {
		t.Fatalf("sacrifice options %+v, want exactly the three candidates", ds.Options)
	}
	submitChoices(t, e, chosen...)
	passUntilStackEmpty(t, e, 20)
	for i, id := range ids {
		if e.G.Obj(id).Zone != state.ZGraveyard {
			t.Fatalf("permanent %d zone=%s, want graveyard (all three sacrificed)", i, e.G.Obj(id).Zone)
		}
	}
	if e.G.Obj(spell).Zone != state.ZBattlefield {
		t.Fatalf("Dargo zone=%s, want battlefield (cast resolved)", e.G.Obj(spell).Zone)
	}
	// Paid {R}: pool {1}{R} minus the one red pip leaves {1}.
	if pool := e.G.Players[0].Pool; pool.Total() != 1 || pool[state.MR] != 0 {
		t.Fatalf("pool after payment=%+v, want 1 colourless ({R} paid from {1}{R})", pool)
	}
}

// TestDargoWithheldWhenNoSacrificeCountIsPayable is the fail-closed half: one
// eligible permanent and {1}{R} cannot pay any legal announcement (X=1 reduces
// {6}{R} only to {4}{R}), so no cast option may be offered. Without the
// precondition split this could pass vacuously, so the eligible permanent and
// the shortfall are both asserted.
func TestDargoWithheldWhenNoSacrificeCountIsPayable(t *testing.T) {
	t.Parallel()
	e, spell, ids := dargoEngine(t, []string{
		"Name:Anvil\nTypes:Artifact\nOracle:x\n",
	}, "1R")
	if e.G.Obj(ids[0]).Zone != state.ZBattlefield {
		t.Fatalf("precondition: the sole eligible permanent is not on the battlefield")
	}
	// Best case X=1 reduces {6}{R} to {4}{R}, still above {1}{R}.
	if pool := e.G.Players[0].Pool; pool.Total() >= 5 {
		t.Fatalf("precondition: pool %+v totals %d, want below even the reduced {4}{R}=5", pool, pool.Total())
	}
	spellID := spell
	d := e.Pending()
	if d == nil {
		t.Fatal("precondition: no pending decision to inspect")
	}
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == spellID {
			t.Fatalf("cast offered at option %+v though no sacrifice announcement is payable", o)
		}
	}
}
