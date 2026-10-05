package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

func TestPumpAllOpponentScopeRestrictsSweep(t *testing.T) {
	g, ids := board(t)
	h := &fakeHost{g: g}
	for _, name := range []string{"myBear", "theirBig"} {
		if o := g.Obj(ids[name]); o == nil || o.Zone != state.ZBattlefield {
			t.Fatalf("precondition: %s is not on the battlefield", name)
		}
	}
	if g.Obj(ids["myBear"]).Controller == g.Obj(ids["theirBig"]).Controller {
		t.Fatal("precondition: creatures share a controller")
	}
	Resolve(h, playerCtx(1), sa(t, "SP$ PumpAll | ValidTgts$ Opponent | ValidCards$ Creature | NumAtt$ +2 | NumDef$ +2"))
	got := map[state.ObjID]int{}
	for _, ce := range h.continuous {
		if ce.Layer == state.LPT && ce.Sub == state.SubModify {
			got[ce.Source]++
		}
	}
	if got[ids["theirBig"]] != 1 {
		t.Fatalf("target opponent's creature pump count = %d, want 1", got[ids["theirBig"]])
	}
	for _, name := range []string{"myBear", "myFlier"} {
		if got[ids[name]] != 0 {
			t.Errorf("non-target %s received %d pumps, want 0", name, got[ids[name]])
		}
	}
}

func TestDestroyAllOpponentScopeRestrictsSweep(t *testing.T) {
	g, ids := board(t)
	h := &fakeHost{g: g}
	for _, name := range []string{"myBear", "theirBig"} {
		if o := g.Obj(ids[name]); o == nil || o.Zone != state.ZBattlefield {
			t.Fatalf("precondition: %s is not on the battlefield", name)
		}
	}
	if g.Obj(ids["myBear"]).Controller == g.Obj(ids["theirBig"]).Controller {
		t.Fatal("precondition: creatures share a controller")
	}
	Resolve(h, playerCtx(1), sa(t, "SP$ DestroyAll | ValidTgts$ Opponent | ValidCards$ Creature"))
	if o := g.Obj(ids["theirBig"]); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("target opponent's creature was not moved to graveyard")
	}
	for _, name := range []string{"myBear", "myFlier"} {
		if o := g.Obj(ids[name]); o == nil || o.Zone != state.ZBattlefield {
			t.Errorf("non-target %s left the battlefield", name)
		}
	}
}
