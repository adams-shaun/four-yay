package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

// Behavioural leaves for the generic RememberTargets$ home (Forge's
// AbilityUtils.handleRemembering) at the effects tier, where the persistent
// half the engine folds from a Choose event is directly observable. These
// cover the APIs that had no reader before the generic home (Tap) and the
// player-target + ForgetOtherTargets$ shape the LoseLife carriers use.

// TestRememberTargetsGenericRecordsBothHalves: a Tap SA with
// RememberTargets$ True remembers its chosen object in BOTH halves -- the
// Ctx.Remembered list a chained sub-ability reads, and the source's
// event-backed persistent list that Card.IsRemembered and
// Count$RememberedSize read later.
func TestRememberTargetsGenericRecordsBothHalves(t *testing.T) {
	g, ids := board(t)
	h := &fakeHost{g: g}
	source := g.Obj(ids["myLand"])
	if source == nil || source.Zone != state.ZBattlefield {
		t.Fatal("remembering source must start on the battlefield")
	}
	ctx := &Ctx{Source: source.ID, Controller: 0,
		Targets: []state.Target{{Obj: ids["myBear"]}}, TargetsOffered: true}
	Resolve(h, ctx, sa(t, "SP$ Tap | ValidTgts$ Creature | RememberTargets$ True"))
	// The body ran (precondition for the remember assertions).
	if !g.Obj(ids["myBear"]).Tapped {
		t.Fatal("the Tap body must have run (bear not tapped)")
	}
	if len(ctx.Remembered) != 1 || ctx.Remembered[0].Obj != ids["myBear"] {
		t.Fatalf("ctx remembered = %#v, want the tapped bear", ctx.Remembered)
	}
	if len(source.Remembered) != 1 || source.Remembered[0].Obj != ids["myBear"] {
		t.Fatalf("source remembered = %#v, want the tapped bear", source.Remembered)
	}
}

// TestRememberTargetsGenericForgetsOtherTargetsForPlayerTargets: a LoseLife
// SA with RememberTargets$ True + ForgetOtherTargets$ True (the Laquatus's
// Champion / Soul Scourge shape) clears the prior remembered set in both
// halves, then records the chosen PLAYER -- the player entry must ride the
// source's persistent list too (eventRemember's PlayerRef encoding).
func TestRememberTargetsGenericForgetsOtherTargetsForPlayerTargets(t *testing.T) {
	g, ids := board(t)
	h := &fakeHost{g: g}
	source := g.Obj(ids["myLand"])
	if source == nil || source.Zone != state.ZBattlefield {
		t.Fatal("remembering source must start on the battlefield")
	}
	old := ids["theirBig"]
	source.Remembered = []state.Target{{Obj: old}}
	ctx := &Ctx{Source: source.ID, Controller: 0,
		Targets:    []state.Target{{Player: 1, IsPlayer: true}},
		Remembered: []state.Target{{Obj: old}}, TargetsOffered: true}
	Resolve(h, ctx, sa(t, "SP$ LoseLife | ValidTgts$ Player | LifeAmount$ 1 | RememberTargets$ True | ForgetOtherTargets$ True"))
	// The body ran (precondition): seat 1 lost 1 life.
	if g.Players[1].Life != 19 {
		t.Fatalf("seat 1 life = %d, want 19 (the LoseLife body must have run)", g.Players[1].Life)
	}
	if len(ctx.Remembered) != 1 || !ctx.Remembered[0].IsPlayer || ctx.Remembered[0].Player != 1 {
		t.Fatalf("ctx remembered = %#v, want exactly the chosen player 1", ctx.Remembered)
	}
	if len(source.Remembered) != 1 || !source.Remembered[0].IsPlayer || source.Remembered[0].Player != 1 {
		t.Fatalf("source remembered = %#v, want exactly the chosen player 1", source.Remembered)
	}
}
