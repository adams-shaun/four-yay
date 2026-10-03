package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// A controller read takes the live association, not the exile-gated object
// selector; it does not use the resolving ability's controller or card owner.
func TestDefinedImprintedControllerReadsPersistentPile(t *testing.T) {
	h := newHost(t, 3)
	src := h.g.AddObject(mkCard(t, "Name:Imprinter\nTypes:Enchantment\nOracle:x\n"), 0)
	linked := h.g.AddObject(mkCard(t, "Name:Linked\nTypes:Enchantment\nOracle:x\n"), 1)
	h.Emit(events.Event{Kind: events.MoveZone, Obj: src.ID, From: state.ZLibrary, To: state.ZBattlefield})
	h.Emit(events.Event{Kind: events.MoveZone, Obj: linked.ID, From: state.ZLibrary, To: state.ZBattlefield})
	h.Emit(events.Event{Kind: events.Imprint, Obj: src.ID, IDs: []state.ObjID{linked.ID}})
	// The imprinted card is controlled by a seat other than source or owner.
	h.Emit(events.Event{Kind: events.ControlChange, Obj: linked.ID, Player: 2})
	if o := h.g.Obj(linked.ID); o == nil || o.Zone != state.ZBattlefield || o.Controller != 2 || o.Owner != 1 || o.Controller == h.g.Obj(src.ID).Controller {
		t.Fatalf("precondition: linked card = %+v; want battlefield owner 1, controller 2, distinct from source", o)
	}
	if ids := h.g.Obj(src.ID).Imprinted; len(ids) != 1 || ids[0] != linked.ID {
		t.Fatalf("precondition: imprint order = %v", ids)
	}
	c := &Ctx{Source: src.ID, Controller: 0}
	want := state.Target{Player: 2, IsPlayer: true}
	if got, ok := definedSpec(h, c, "ImprintedController"); !ok || len(got) != 1 || got[0] != want {
		t.Fatalf("definedSpec ImprintedController = %v ok=%v, want [%v]", got, ok, want)
	}
	if got := Defined(h, c, sa(t, "DB$ DealDamage | Defined$ ImprintedController")); len(got) != 1 || got[0] != want {
		t.Fatalf("Defined$ ImprintedController = %v, want [%v]", got, want)
	}
}
