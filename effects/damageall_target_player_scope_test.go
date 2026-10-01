package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

// The DamageAll object sweep filters on ValidCards$ only. For the affected
// corpus shape the printed scope ("each creature target player controls") is
// carried by a player-kind ValidTgts$ (Player/Opponent), and the sweep must
// read it or every seat's creatures take the damage (Aggravate, Simoon,
// Chandra, Bold Pyromancer, ...). These leaves pin the scope.
//
// board(t) gives us the two-controller board this needs: myBear/myFlier on
// seat 0, theirBig on seat 1 (effects/filter_test.go:10).

// playerCtx is a resolution whose target ask has already been answered: the
// recorded target is seat p, marked TargetsOffered so the registry's target
// pre-ask does not re-pose (and overwrite it) at this depth.
func playerCtx(p state.PlayerID) *Ctx {
	return &Ctx{Controller: 0, TargetsOffered: true,
		Targets: []state.Target{{Player: p, IsPlayer: true}}}
}

// TestDamageAllTargetPlayerScopeRestrictsSweep is the plain Aggravate shape:
// ValidTgts$ Player | ValidCards$ Creature. Seat 1's creature must take the
// damage; seat 0's creatures must take none.
func TestDamageAllTargetPlayerScopeRestrictsSweep(t *testing.T) {
	g, ids := board(t)
	h := &fakeHost{g: g}
	// Precondition: the objects under assertion are on the battlefield the
	// sweep reads, and the two seats' creatures are different objects with
	// different controllers -- otherwise the assertion below is vacuous.
	for _, name := range []string{"myBear", "myFlier", "theirBig"} {
		if o := g.Obj(ids[name]); o == nil || o.Zone != state.ZBattlefield {
			t.Fatalf("precondition: %s is not on the battlefield", name)
		}
	}
	if g.Obj(ids["myBear"]).Controller == g.Obj(ids["theirBig"]).Controller {
		t.Fatal("precondition: myBear and theirBig share a controller")
	}
	Resolve(h, playerCtx(1),
		sa(t, "SP$ DamageAll | ValidTgts$ Player | ValidCards$ Creature | NumDmg$ 1"))
	if got := g.Obj(ids["theirBig"]).Damage; got != 1 {
		t.Fatalf("target player's creature damage = %d, want 1", got)
	}
	for _, name := range []string{"myBear", "myFlier"} {
		if got := g.Obj(ids[name]).Damage; got != 0 {
			t.Errorf("%s (seat 0, not the target player) damage = %d, want 0", name, got)
		}
	}
}

// TestDamageAllTargetPlayerScopeSpansCommaAlternatives is the Chandra, Bold
// Pyromancer shape: ValidCards$ Creature,Planeswalker. The scope must apply
// to BOTH alternatives; a spec-string append shortcut would scope only the
// last one.
func TestDamageAllTargetPlayerScopeSpansCommaAlternatives(t *testing.T) {
	g, ids := board(t)
	h := &fakeHost{g: g}
	if o := g.Obj(ids["theirBig"]); o == nil || o.Zone != state.ZBattlefield {
		t.Fatal("precondition: theirBig is not on the battlefield")
	}
	if o := g.Obj(ids["myWalker"]); o == nil || o.Zone != state.ZBattlefield {
		t.Fatal("precondition: myWalker is not on the battlefield")
	}
	if g.Obj(ids["myWalker"]).Controller == g.Obj(ids["theirBig"]).Controller {
		t.Fatal("precondition: myWalker and theirBig share a controller")
	}
	Resolve(h, playerCtx(1),
		sa(t, "SP$ DamageAll | ValidTgts$ Player | ValidCards$ Creature,Planeswalker | NumDmg$ 3"))
	// theirBig (seat 1) is a Creature; myWalker (seat 1-irrelevant) is on
	// seat 0 so it must be skipped. The target player's own creature proves
	// the first alternative is still swept -- a fix that skipped the whole
	// sweep would fail here.
	if got := g.Obj(ids["theirBig"]).Damage; got != 3 {
		t.Fatalf("target player's creature damage = %d, want 3", got)
	}
	if got := g.Obj(ids["myWalker"]).Damage; got != 0 {
		t.Errorf("seat 0's planeswalker took %d, want 0 (scope must cover both alternatives)", got)
	}
	if got := g.Obj(ids["myBear"]).Damage; got != 0 {
		t.Errorf("seat 0's creature took %d, want 0", got)
	}
}

// TestDamageAllOpponentScopeRestrictsSweep is the Simoon / Savage Alliance
// shape: ValidTgts$ Opponent | ValidCards$ Creature.
func TestDamageAllOpponentScopeRestrictsSweep(t *testing.T) {
	g, ids := board(t)
	h := &fakeHost{g: g}
	if o := g.Obj(ids["theirBig"]); o == nil || o.Zone != state.ZBattlefield {
		t.Fatal("precondition: theirBig is not on the battlefield")
	}
	Resolve(h, playerCtx(1),
		sa(t, "SP$ DamageAll | ValidTgts$ Opponent | ValidCards$ Creature | NumDmg$ 2"))
	if got := g.Obj(ids["theirBig"]).Damage; got != 2 {
		t.Fatalf("target opponent's creature damage = %d, want 2", got)
	}
	for _, name := range []string{"myBear", "myFlier"} {
		if got := g.Obj(ids[name]).Damage; got != 0 {
			t.Errorf("%s (seat 0, not the target opponent) damage = %d, want 0", name, got)
		}
	}
}

// TestDamageAllNonPlayerTargetsSweepsAllControllers is the regression leaf:
// a non-player ValidTgts$ (the shroud-test shape, ValidTgts$ Creature) must
// NOT scope the sweep. Every matching creature takes the damage.
func TestDamageAllNonPlayerTargetsSweepsAllControllers(t *testing.T) {
	g, ids := board(t)
	h := &fakeHost{g: g}
	Resolve(h, &Ctx{Controller: 0, TargetsOffered: true,
		Targets: []state.Target{{Obj: ids["theirBig"]}}},
		sa(t, "SP$ DamageAll | ValidTgts$ Creature | ValidCards$ Creature | NumDmg$ 1"))
	for _, name := range []string{"myBear", "myFlier", "theirBig"} {
		if got := g.Obj(ids[name]).Damage; got != 1 {
			t.Errorf("%s damage = %d, want 1 (Creature ValidTgts$ must not scope the sweep)", name, got)
		}
	}
}

// TestDamageAllTargetedPlayerCtrlSpecStillSweeps is the chandras_flame_wave
// shape: the controller restriction already lives in ValidCards$ as
// Creature.TargetedPlayerCtrl. The scope branch must not double-apply and
// must leave the worked path intact.
func TestDamageAllTargetedPlayerCtrlSpecStillSweeps(t *testing.T) {
	g, ids := board(t)
	h := &fakeHost{g: g}
	if o := g.Obj(ids["theirBig"]); o == nil || o.Zone != state.ZBattlefield {
		t.Fatal("precondition: theirBig is not on the battlefield")
	}
	Resolve(h, playerCtx(1),
		sa(t, "SP$ DamageAll | ValidTgts$ Player | ValidCards$ Creature.TargetedPlayerCtrl | NumDmg$ 2"))
	if got := g.Obj(ids["theirBig"]).Damage; got != 2 {
		t.Fatalf("target player's creature (TargetedPlayerCtrl spec) damage = %d, want 2", got)
	}
	for _, name := range []string{"myBear", "myFlier"} {
		if got := g.Obj(ids[name]).Damage; got != 0 {
			t.Errorf("%s damage = %d, want 0", name, got)
		}
	}
}
