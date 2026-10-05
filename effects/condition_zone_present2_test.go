package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestConditionZoneStackGift pins the ConditionZone$ Stack gift-promise gate
// (Blooming Blast, Longstalk Brawl, Mind Spiral, Bilbo's Gambit): the gate
// reads the SOURCE ON THE STACK, so ConditionPresent$ Card.Self+PromisedGift
// with ConditionCompare$ EQ1 is met exactly when the resolving spell carries
// the CR 702.168 promise. Before this the bare-present default group was the
// battlefield, the stack self never matched, and the gate was unresolved
// (fail-open) -- so the promised rider ran (or not) regardless.
func TestConditionZoneStackGift(t *testing.T) {
	h := newHost(t, 2)
	blast := mkCard(t, "Name:Blast\nTypes:Instant\nOracle:x\n")
	sID := h.g.AddObject(blast, 0).ID
	h.g.Obj(sID).Zone = state.ZStack // the spell is resolving from the stack

	sa := sa(t, "DB$ DealDamage | ConditionZone$ Stack | ConditionPresent$ Card.Self+PromisedGift | ConditionCompare$ EQ1")
	ctx := &Ctx{Controller: 0, Source: sID}

	// Precondition: the source really is on the stack and carries no promise.
	if h.g.Obj(sID).Zone != state.ZStack {
		t.Fatalf("source must be on the stack for the ConditionZone gate")
	}
	if h.g.Obj(sID).CastFlags&state.FlagPromisedGift != 0 {
		t.Fatalf("source must not be promised before the test sets it")
	}
	if met, resolved := conditionMet(h, ctx, sa); met || !resolved {
		t.Fatalf("no promise: met=%v resolved=%v, want false true", met, resolved)
	}
	h.g.Obj(sID).CastFlags |= state.FlagPromisedGift
	if met, resolved := conditionMet(h, ctx, sa); !met || !resolved {
		t.Fatalf("promised: met=%v resolved=%v, want true true", met, resolved)
	}
}

// TestConditionZoneGraveyardCount pins the ConditionZone$ Graveyard count
// gate (Kytheon's Tactics, Bloodsworn Squire, Eye of Jace): the group is the
// named zone, so ConditionPresent$ Instant.YouOwn,Sorcery.YouOwn with
// ConditionCompare$ GE2 is met by two instant/sorcery cards in the
// controller's graveyard and not by one.
func TestConditionZoneGraveyardCount(t *testing.T) {
	h := newHost(t, 2)
	spellA := mkCard(t, "Name:Bolt\nTypes:Instant\nOracle:x\n")
	spellB := mkCard(t, "Name:Ritual\nTypes:Sorcery\nOracle:x\n")
	land := mkCard(t, "Name:Mountain\nTypes:Land\nOracle:x\n")
	aID := h.g.AddObject(spellA, 0).ID
	h.g.Obj(aID).Zone = state.ZGraveyard
	bID := h.g.AddObject(spellB, 0).ID
	h.g.Obj(bID).Zone = state.ZGraveyard
	mID := h.g.AddObject(land, 0).ID
	h.g.Obj(mID).Zone = state.ZGraveyard

	sa := sa(t, "DB$ PumpAll | ConditionPresent$ Instant.YouOwn,Sorcery.YouOwn | ConditionZone$ Graveyard | ConditionCompare$ GE2")
	ctx := &Ctx{Controller: 0, Source: 0}

	// Precondition: exactly the two spells are in the graveyard (plus the
	// land, which the spec must NOT count).
	for _, id := range []state.ObjID{aID, bID, mID} {
		if h.g.Obj(id).Zone != state.ZGraveyard {
			t.Fatalf("setup object %d must be in the graveyard", id)
		}
	}
	if met, resolved := conditionMet(h, ctx, sa); !met || !resolved {
		t.Fatalf("two spells in graveyard vs GE2: met=%v resolved=%v, want true true", met, resolved)
	}
	// Move one spell away: the count drops below the threshold.
	h.g.Obj(aID).Zone = state.ZExile
	if met, resolved := conditionMet(h, ctx, sa); met || !resolved {
		t.Fatalf("one spell in graveyard vs GE2: met=%v resolved=%v, want false true", met, resolved)
	}
}

// TestConditionPresent2AndTargetedBy pins Forge's SECOND presence group
// (ConditionPresent2$) AND-ed with the first, plus the targetedBy qualifier,
// on Super-Adaptoid's exact shape: the gate is met only when a creature this
// ability targeted has the named keyword AND the source itself lacks it.
func TestConditionPresent2AndTargetedBy(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	h := newHost(t, 2)
	g := h.g

	add := func(name string) state.ObjID {
		c, ok := reg.Lookup(name)
		if !ok {
			t.Fatalf("corpus has no %q", name)
		}
		id := g.AddObject(c, 0).ID
		g.Obj(id).Zone = state.ZBattlefield
		return id
	}
	selfID := add("Grizzly Bears") // no Haste
	fastID := add("Raging Goblin") // Haste
	slowID := add("Grizzly Bears") // no Haste

	// Preconditions: the keyword split the gate depends on is real.
	if !objectHasKeyword(g.Obj(fastID), "Haste") {
		t.Fatalf("Raging Goblin must have Haste")
	}
	if objectHasKeyword(g.Obj(selfID), "Haste") || objectHasKeyword(g.Obj(slowID), "Haste") {
		t.Fatalf("Grizzly Bears must not have Haste")
	}

	sa := sa(t, "DB$ PutCounter | ConditionPresent$ Creature.targetedBy+withHaste | ConditionPresent2$ Card.Self+withoutHaste")
	ctx := &Ctx{Controller: 0, Source: selfID}

	// Neither targeted: the first group is empty -> denied.
	if met, resolved := conditionMet(h, ctx, sa); met || !resolved {
		t.Fatalf("nothing targeted: met=%v resolved=%v, want false true", met, resolved)
	}
	// A targeted Haste creature, self without Haste -> met.
	ctx.Targets = []state.Target{{Obj: fastID}}
	if met, resolved := conditionMet(h, ctx, sa); !met || !resolved {
		t.Fatalf("targeted haste creature: met=%v resolved=%v, want true true", met, resolved)
	}
	// A targeted non-Haste creature -> first group empty -> denied.
	ctx.Targets = []state.Target{{Obj: slowID}}
	if met, resolved := conditionMet(h, ctx, sa); met || !resolved {
		t.Fatalf("targeted non-haste creature: met=%v resolved=%v, want false true", met, resolved)
	}
	// Self HAS Haste -> the second group denies even with a Haste target.
	// Use a corpus Haste source so the second leg is exercised for real (the
	// engine's Haste is a layer keyword, not a cast flag).
	ctx.Targets = []state.Target{{Obj: fastID}}
	ctx.Source = add("Raging Goblin")
	if met, resolved := conditionMet(h, ctx, sa); met || !resolved {
		t.Fatalf("source with haste: met=%v resolved=%v, want false true", met, resolved)
	}
}
