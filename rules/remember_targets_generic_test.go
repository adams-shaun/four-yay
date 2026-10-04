package rules

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// Behavioural leaves for the generic RememberTargets$ home (Forge's
// AbilityUtils.handleRemembering): effects.Resolve now records the chosen
// targets of EVERY SA that carries RememberTargets$ True in both halves --
// Ctx.Remembered and the source's event-backed list -- so the APIs that used
// to be unread (PutCounter, DealDamage, and the rest of the census) finally
// feed a later `Defined$ Remembered` / `RememberedController` link. Each
// protagonist is a REAL compiled corpus card driven through effects.Resolve,
// the direct effect-entry harness effects/primitives_test.go and
// rules/regeneration_test.go use.

// corpusAbility returns the first root ability of the named corpus card whose
// API matches, failing loudly when the card or the ability is absent (a
// corpus-pin move is a test premise change, not something to paper over).
func corpusAbility(t *testing.T, reg *cards.Registry, name, api string) *cards.SA {
	t.Helper()
	c := lookup(t, reg, name)
	for _, sa := range c.Faces[0].Abilities {
		if sa != nil && sa.API == api {
			return sa
		}
	}
	t.Fatalf("corpus card %q has no root %s ability", name, api)
	return nil
}

// TestPutCounterRememberTargetsFeedsTheChain pins the generic home on a
// PutCounter carrier: Gore Vassal's ability puts a -1/-1 counter on the
// target, remembers it, and the chained DBRegenerate reads `Defined$
// Remembered` with X = `Remembered$CardToughness` to shield the creature
// while its (reduced) toughness is at least 1. Before the generic home the
// Remembered set was empty, X was 0, and no shield was granted.
func TestPutCounterRememberTargetsFeedsTheChain(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	put := corpusAbility(t, reg, "Gore Vassal", "PutCounter")
	e := layerEngine(t)
	source := e.G.AddObject(lookup(t, reg, "Gore Vassal"), 0).ID
	bear := onBoard(t, e, 1, "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:3/3\nOracle:x\n")
	// Precondition: the target is a live 3/3 on the battlefield, so the
	// -1/-1 counter leaves toughness 2 and the chain's X gate can pass.
	if o := e.G.Obj(bear); o == nil || o.Zone != state.ZBattlefield || e.Toughness(bear) != 3 {
		t.Fatalf("target not a live 3/3: %+v", o)
	}
	effects.Resolve(e, &effects.Ctx{Source: source, Controller: 0,
		Targets: []state.Target{{Obj: bear}}, TargetsOffered: true}, put)

	// The body ran (the counter landed).
	if o := e.G.Obj(bear); o == nil || o.Counter("M1M1") != 1 {
		t.Fatalf("target counter = %v, want one -1/-1 counter (the PutCounter body must have run)", e.G.Obj(bear))
	}
	// The downstream read: `Defined$ Remembered` shielded the bear while its
	// reduced toughness (2) is at least 1. Gore Vassal's chain ends with
	// `DB$ Cleanup | ClearRemembered$ True`, so the persistent half is gone by
	// now -- the shield is the observable that proves the generic recorder fed
	// the Ctx.Remembered half the chained DBRegenerate reads.
	if got := e.G.Obj(bear).Counter("Shield"); got != 1 {
		t.Fatalf("target regeneration shields = %d, want 1 -- the chained DBRegenerate did not see the remembered target", got)
	}
}

// TestDealDamageRememberTargetsScopesTheAnimateAll pins the generic home on a
// DealDamage carrier: Craterous Stomp deals 3 to a creature an opponent
// controls, then its DBAnimateAll makes every OTHER creature that player
// controls a Coward (`ValidCards$ Creature.ControlledBy RememberedController
// +!IsRemembered`). With an empty Remembered set RememberedController names
// nobody and nothing is animated; with the generic home the target's
// controller is named and the target itself is excluded by !IsRemembered.
func TestDealDamageRememberTargetsScopesTheAnimateAll(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	deal := corpusAbility(t, reg, "Craterous Stomp", "DealDamage")
	e := layerEngine(t)
	source := e.G.AddObject(lookup(t, reg, "Craterous Stomp"), 0).ID
	victim := onBoard(t, e, 1, "Name:Victim\nManaCost:1 G\nTypes:Creature Bear\nPT:4/4\nOracle:x\n")
	other := onBoard(t, e, 1, "Name:Other\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	// Precondition: two distinct live creatures under one opponent, and the
	// victim survives the 3 damage (4/4), so the AnimateAll link runs on a
	// board where "each other creature that player controls" is non-empty.
	if e.G.Obj(victim) == nil || e.G.Obj(other) == nil || victim == other {
		t.Fatalf("fixture needs two distinct live creatures")
	}
	effects.Resolve(e, &effects.Ctx{Source: source, Controller: 0,
		Targets: []state.Target{{Obj: victim}}, TargetsOffered: true}, deal)

	if got := e.G.Obj(victim).Damage; got != 3 {
		t.Fatalf("victim damage = %d, want 3 (the DealDamage body must have run)", got)
	}
	e.refreshDerivedTypes()
	if ty := e.Derived(other).Types; !slices.Contains(ty, "Coward") {
		t.Fatalf("the target's other creature derives %v, want it to include Coward from the remembered controller", ty)
	}
	if ty := e.Derived(victim).Types; slices.Contains(ty, "Coward") {
		t.Fatalf("the damage target derives %v, want it excluded by !IsRemembered", ty)
	}
}
