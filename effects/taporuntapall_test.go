package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

func TestTapOrUntapAllTargetPlayerScope(t *testing.T) {
	g, ids := board(t)
	h := &fakeHost{g: g}
	for _, name := range []string{"myBear", "theirBig"} {
		if o := g.Obj(ids[name]); o == nil || o.Zone != state.ZBattlefield {
			t.Fatalf("precondition: %s is not on the battlefield", name)
		}
	}
	if g.Obj(ids["myBear"]).Controller == g.Obj(ids["theirBig"]).Controller {
		t.Fatal("precondition: myBear and theirBig share a controller")
	}
	if g.Obj(ids["myBear"]).Tapped || g.Obj(ids["theirBig"]).Tapped {
		t.Fatal("precondition: both permanents should start untapped")
	}

	Resolve(h, playerCtx(1), sa(t, "SP$ TapOrUntapAll | ValidTgts$ Player | ValidCards$ Creature"))
	if !g.Obj(ids["theirBig"]).Tapped {
		t.Error("target player's creature was not tapped")
	}
	if g.Obj(ids["myBear"]).Tapped {
		t.Error("non-target player's creature was tapped")
	}
}

func TestTapOrUntapAllTapAndUntap(t *testing.T) {
	g, ids := board(t)
	h := &fakeHost{g: g}
	for _, name := range []string{"myBear", "theirBig"} {
		if o := g.Obj(ids[name]); o == nil || o.Zone != state.ZBattlefield {
			t.Fatalf("precondition: %s is not on the battlefield", name)
		}
	}
	if g.Obj(ids["myBear"]).Tapped || g.Obj(ids["theirBig"]).Tapped {
		t.Fatal("precondition: both permanents should start untapped")
	}
	// Establish the mixed input states through the ordinary tap event path.
	h.EmitTap(ids["theirBig"], 0, false)
	if !g.Obj(ids["theirBig"]).Tapped || g.Obj(ids["myBear"]).Tapped {
		t.Fatal("precondition: expected one tapped and one untapped battlefield creature")
	}

	Resolve(h, &Ctx{Controller: 0}, sa(t, "SP$ TapOrUntapAll | ValidCards$ Creature"))
	if !g.Obj(ids["myBear"]).Tapped {
		t.Error("untapped creature was not tapped")
	}
	if g.Obj(ids["theirBig"]).Tapped {
		t.Error("tapped creature was not untapped")
	}
}
