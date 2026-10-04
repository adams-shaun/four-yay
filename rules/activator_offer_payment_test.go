package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// An opponent-permitted source must participate not just in priority's
// activate option, but in the membership/probe used to open a payment window.
func TestActivatorOfferManaSourceInPaymentWindow(t *testing.T) {
	t.Parallel()
	const src = `Name:Opponent Mana Cache
ManaCost:0
Types:Artifact
A:AB$ Mana | Cost$ T | Produced$ C | Activator$ Player | SpellDescription$ Add {C}. Any player may activate this ability.
Oracle:x
`
	e, _, id := newFixtureDeck(t, 8620, src)
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZBattlefield})
	if got := e.controllerOf(id); got != 0 || e.G.Obj(id).Zone != state.ZBattlefield {
		t.Fatalf("fixture precondition: source controller=%d zone=%s", got, e.G.Obj(id).Zone)
	}
	if e.G.Obj(id).Tapped {
		t.Fatal("fixture precondition: opponent mana source is tapped")
	}
	if !e.activatorAllows(1, id, e.G.Obj(id).Face().Abilities[0]) {
		t.Fatal("fixture precondition: Activator$ Player does not permit payer 1")
	}
	if !e.untappedManaSource(1, id) {
		t.Fatal("fixture precondition: source is not individually usable by payer 1")
	}
	if !e.hasUntappedManaSource(1) {
		t.Fatal("opponent-permitted source was omitted from payment-window availability")
	}
	units := e.windowManaUnits(1)
	found := false
	for _, u := range units {
		if u.ID == id && len(u.Alts) > 0 {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("opponent-permitted source was omitted from payment-window pricing: %+v", units)
	}
	if got := e.AvailableMana(1)[5]; got != 1 {
		t.Fatalf("payer's public available colorless mana = %d, want 1", got)
	}
}
