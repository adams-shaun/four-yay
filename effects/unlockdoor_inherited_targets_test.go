package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// TestUnlockDoorNonTargetedSubDoesNotUseOuterTargets guards the boundary
// between a targeted parent and a non-targeted UnlockDoor sub-ability. The
// parent's target is an opponent's Room; the sub must instead use the
// controller's Room pool. The preconditions make both outcomes observable.
func TestUnlockDoorNonTargetedSubDoesNotUseOuterTargets(t *testing.T) {
	h := newHost(t, 2)
	own := h.g.AddObject(testRoomDoorCard(t, "Own Room", "Own Chamber"), 0).ID
	outer := h.g.AddObject(testRoomDoorCard(t, "Outer Room", "Outer Chamber"), 1).ID
	for _, id := range []state.ObjID{own, outer} {
		h.g.Obj(id).Zone = state.ZBattlefield
		if !roomHasLockedDoor(h.g.Obj(id), h.g.Obj(id).Controller) {
			t.Fatalf("precondition: Room %d must have a locked door", id)
		}
	}

	unregister("UnlockDoorTestParent")
	Register("UnlockDoorTestParent", func(Host, *Ctx, *cards.SA) {})
	t.Cleanup(func() { unregister("UnlockDoorTestParent") })
	parent := &cards.SA{Kind: "DB", API: "UnlockDoorTestParent", Line: "1",
		Params: map[string]string{"ValidTgts": "Room"}}
	child := &cards.SA{Kind: "DB", API: "UnlockDoor", Line: "2",
		Params: map[string]string{"Mode": "Unlock"}}
	parent.Sub = child

	Resolve(h, &Ctx{Source: own, Controller: 0,
		Targets: []state.Target{{Obj: outer}}, TargetsOffered: true, OfferedSA: parent}, parent)

	if !h.g.Obj(own).Unlocked {
		t.Fatal("non-targeted UnlockDoor sub used its parent's opponent target instead of the controlled-Room pool")
	}
	if !roomHasLockedDoor(h.g.Obj(outer), 1) {
		t.Fatal("the outer target Room should remain locked; the sub must not act on the parent's target")
	}
	if got := doorUnlockEvents(h); len(got) != 1 || got[0].Obj != own {
		t.Fatalf("DoorUnlock events = %+v, want exactly the controlled Room %d", got, own)
	}
}
