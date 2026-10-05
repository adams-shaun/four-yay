package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

// A ChangeZone target with the wrong Origin$ is still chosen. The mover must
// leave it in place, but the shared post-body recorder must remember it.
func TestChangeZoneRememberTargetsChosenDespiteOriginMismatch(t *testing.T) {
	g, ids := board(t)
	h := &fakeHost{g: g}
	source := g.Obj(ids["myLand"])
	target := g.Obj(ids["myBear"])
	movable := g.Obj(ids["myInstant"])
	if source == nil || source.Zone != state.ZBattlefield || target == nil || target.Zone != state.ZBattlefield ||
		movable == nil || movable.Zone != state.ZGraveyard {
		t.Fatal("source and mismatched target must start on battlefield, movable card in graveyard")
	}
	ctx := &Ctx{Source: source.ID, Controller: 0,
		Targets: []state.Target{{Obj: movable.ID}, {Obj: target.ID}}, TargetsOffered: true}
	Resolve(h, ctx, sa(t, "SP$ ChangeZone | ValidTgts$ Card | Origin$ Graveyard | Destination$ Exile | RememberTargets$ True"))
	if movable.Zone != state.ZExile || target.Zone != state.ZBattlefield {
		t.Fatalf("ChangeZone must move eligible card and spare mismatched target: zones %v, %v", movable.Zone, target.Zone)
	}
	if len(ctx.Remembered) != 2 || ctx.Remembered[0].Obj != movable.ID || ctx.Remembered[1].Obj != target.ID {
		t.Fatalf("ctx remembered = %#v, want both chosen targets", ctx.Remembered)
	}
	if len(source.Remembered) != 2 || source.Remembered[0].Obj != movable.ID || source.Remembered[1].Obj != target.ID {
		t.Fatalf("source remembered = %#v, want both chosen targets", source.Remembered)
	}
}
