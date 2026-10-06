package templates

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// reqKeyOfSub is the key of the card's requirement classified as sub.
func reqKeyOfSub(t *testing.T, reg *cards.Registry, name, sub string) string {
	t.Helper()
	c, ok := reg.Lookup(name)
	if !ok {
		t.Fatalf("%s not in the corpus", name)
	}
	for _, r := range levelb.Requirements(c) {
		if r.Sub == sub {
			return r.Key
		}
	}
	t.Fatalf("precondition: %s has no requirement classified %s", name, sub)
	return ""
}

// lastExpects is the assertion list of the item's final step.
func lastExpects(t *testing.T, it oraclegen.Item) []oraclegen.Expect {
	t.Helper()
	last := it.Steps[len(it.Steps)-1]
	if len(last.Expect) != 2 {
		t.Fatalf("final step carries %d expectations, want probe + card: %+v", len(last.Expect), last)
	}
	return last.Expect
}

// flippedFails replays the item with the card-under-test assertion (the
// second one) flipped and returns gorge's fails: the observation must be the
// engine's doing, so the opposite claim has to be refused.
func flippedFails(t *testing.T, it oraclegen.Item, mutate func(*oraclegen.Scenario)) []string {
	t.Helper()
	reg := loadGenRegistry(t)
	sc := it.Scenario
	sc.Steps = append([]oraclegen.Step(nil), sc.Steps...)
	last := &sc.Steps[len(sc.Steps)-1]
	last.Expect = append([]oraclegen.Expect(nil), last.Expect...)
	last.Expect[1].Want = boolPtr(!*last.Expect[1].Want)
	if mutate != nil {
		sc.Setup = cloneOracleSetup(sc.Setup)
		mutate(&sc)
	}
	res, ok := runStatic(reg, sc)
	if !ok {
		t.Fatal("flipped scenario did not run")
	}
	return res.Fails
}

func TestDefenderAttackerIsNotOffered(t *testing.T) {
	reg := loadGenRegistry(t)
	it := staticItemFor(t, reg, "Fog Bank", "combat#0.attack", "combat.attack")
	ex := lastExpects(t, it)
	if ex[0].CanAttack == nil || *ex[0].Want != true || ex[0].CanAttack.Attacker != "p0:"+smallAttackerProbe {
		t.Fatalf("first assertion is not the probe being offered: %+v", ex[0])
	}
	if ex[1].CanAttack == nil || *ex[1].Want != false || ex[1].CanAttack.Attacker != "p0:Fog Bank" {
		t.Fatalf("second assertion is not the Defender not being offered: %+v", ex[1])
	}
	if last := it.Steps[len(it.Steps)-1]; last.Decision != "attackers" || last.Step != "declare-attackers" {
		t.Fatalf("item does not stop at the declare-attackers decision: %+v", last)
	}
	if len(it.Compare) != 0 {
		t.Fatalf("a declare-decision item has no offered snapshot to compare: %v", it.Compare)
	}
	if res, ok := runStatic(reg, it.Scenario); !ok || len(res.Fails) != 0 {
		t.Fatalf("observation does not hold in gorge: %v", res.Fails)
	}
	if fails := flippedFails(t, it, nil); len(fails) == 0 {
		t.Fatal("claiming the Defender is offered was not refused: the item cannot fail")
	}
}

func TestCantBlockSelfBlockerIsNotOffered(t *testing.T) {
	reg := loadGenRegistry(t)
	// The same observation serves the static row and the combat.block row.
	for _, sub := range []string{"static.cant-block-self", "combat.block"} {
		key := reqKeyOfSub(t, reg, "Villainous Ogre", sub)
		it := staticItemFor(t, reg, "Villainous Ogre", key, sub)
		ex := lastExpects(t, it)
		if ex[0].CanBlock == nil || *ex[0].Want != true || ex[0].CanBlock.Blocker != "p0:"+blockerProbe {
			t.Fatalf("%s: first assertion is not the probe blocking: %+v", sub, ex[0])
		}
		if ex[1].CanBlock == nil || *ex[1].Want != false || ex[1].CanBlock.Blocker != "p0:Villainous Ogre" || ex[1].CanBlock.Attacker != ex[0].CanBlock.Attacker {
			t.Fatalf("%s: second assertion is not the card not blocking the same attacker: %+v", sub, ex[1])
		}
		if res, ok := runStatic(reg, it.Scenario); !ok || len(res.Fails) != 0 {
			t.Fatalf("%s: observation does not hold in gorge: %v", sub, res.Fails)
		}
		if fails := flippedFails(t, it, nil); len(fails) == 0 {
			t.Fatalf("%s: claiming the card may block was not refused: the item cannot fail", sub)
		}
	}
}

func TestSelfCantBeBlockedOffersNoBlock(t *testing.T) {
	reg := loadGenRegistry(t)
	it := staticItemFor(t, reg, "Keymaster Rogue", "static#0.0", "static.cant-block-by-self")
	ex := lastExpects(t, it)
	if ex[0].CanBlock == nil || *ex[0].Want != true || ex[0].CanBlock.Attacker != "p0:"+largeAttackerProbe {
		t.Fatalf("probe attacker is not blockable: %+v", ex[0])
	}
	if ex[1].CanBlock == nil || *ex[1].Want != false || ex[1].CanBlock.Attacker != "p0:Keymaster Rogue" || ex[1].CanBlock.Blocker != ex[0].CanBlock.Blocker {
		t.Fatalf("card is not unblockable by the same blocker: %+v", ex[1])
	}
	if res, ok := runStatic(reg, it.Scenario); !ok || len(res.Fails) != 0 {
		t.Fatalf("observation does not hold in gorge: %v", res.Fails)
	}
	if fails := flippedFails(t, it, nil); len(fails) == 0 {
		t.Fatal("claiming the card may be blocked was not refused: the item cannot fail")
	}
}

func TestMinBlockersHidesTheBlockBelowTheBound(t *testing.T) {
	reg := loadGenRegistry(t)
	it := staticItemFor(t, reg, "Rampaging Ceratops", "static#0.0", "static.min-blockers")
	ex := lastExpects(t, it)
	if ex[1].CanBlock == nil || *ex[1].Want != false || ex[1].CanBlock.Attacker != "p0:Rampaging Ceratops" {
		t.Fatalf("card is not unblockable below its bound: %+v", ex[1])
	}
	const bound = 3 // Min$ 3
	if got := len(it.Setup["p1"].Battlefield); got != bound-1 {
		t.Fatalf("p1 fields %d blockers, want %d (one below Min$ %d)", got, bound-1, bound)
	}
	if res, ok := runStatic(reg, it.Scenario); !ok || len(res.Fails) != 0 {
		t.Fatalf("observation does not hold in gorge: %v", res.Fails)
	}
	// The bound is the cause: one more blocker offers the block again.
	if fails := flippedFails(t, it, func(sc *oraclegen.Scenario) {
		p1 := sc.Setup["p1"]
		p1.Battlefield = oraclegen.Repeat(blockerProbe, bound)
		sc.Setup["p1"] = p1
	}); len(fails) != 0 {
		t.Fatalf("with %d blockers the block must be offered: %v", bound, fails)
	}
	if fails := flippedFails(t, it, nil); len(fails) == 0 {
		t.Fatal("claiming the block is offered below the bound was not refused: the item cannot fail")
	}
}
