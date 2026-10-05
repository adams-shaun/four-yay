package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestEndureDepartedStolenPermanentCreatesForAbilityController pins the
// resolving trigger's controller when a stolen permanent has left the field.
// A battlefield departure resets the live object's controller to its owner,
// but CR 701.63's token is created by the ability's controller.
func TestEndureDepartedStolenPermanentCreatesForAbilityController(t *testing.T) {
	h, c := endureHost(t, -1)
	o := h.g.Obj(c.Source)
	// Model a permanent owned by seat 1 and controlled by seat 0 when its
	// Endure trigger resolves. The fixture began under seat 0; set its owner
	// as part of test setup, then use the event path for the control state.
	o.Owner = 1
	o.Controller = 1
	h.Emit(events.Event{Kind: events.ControlChange, Obj: c.Source, Player: 0})
	if o.Controller != 0 || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: stolen permanent is not controlled by seat 0 on the battlefield: %+v", o)
	}
	c.Controller = 0
	h.Emit(events.Event{Kind: events.MoveZone, Obj: c.Source, From: state.ZBattlefield, To: state.ZGraveyard})
	if o.Controller != 1 || o.Zone != state.ZGraveyard {
		t.Fatalf("precondition: departure did not reset controller to owner: %+v", o)
	}

	Resolve(h, c, sa(t, "DB$ Endure | Num$ 2"))

	if h.asks != 0 {
		t.Fatalf("departed Endure posed %d asks, want no choice", h.asks)
	}
	var token *state.Object
	for _, id := range h.g.Zone(state.ZBattlefield, 0) {
		candidate := h.g.Obj(id)
		if candidate != nil && candidate.IsToken && candidate.Face() != nil && candidate.Face().Name == "Spirit Token" {
			token = candidate
		}
	}
	if token == nil {
		t.Fatalf("ability controller seat 0 received no Spirit; battlefield seat 0 = %v", h.g.Zone(state.ZBattlefield, 0))
	}
	if token.Controller != 0 || token.Owner != 0 {
		t.Fatalf("departed Endure Spirit owner/controller = %d/%d, want ability controller 0", token.Owner, token.Controller)
	}
}
