// The Duration$ Permanent move-driven lifetime for Animate/AnimateAll and
// Pump/PumpAll grants (effects/combatfx.go): when the granted object leaves
// the zone it was granted in, every half of the grant ends (CR 400.7 -- the
// object that returns is a new one), via the ExileOnMoved$/Remembered pair
// rules/layers.go's effectMoveSweep reads. A bounced-and-re-entered manland
// is a plain land again; a pumped creature that was bounced does not come
// back still pumped.
//
// The rider-specific lifetime (LeaveBattlefield$ Exile, ag.endOnLeave) is a
// separate, already-implemented shape -- see rules/animate_leavebattlefield_test.go.
package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestPermanentAnimateGrantEndsOnZoneChange: Stalking Stones's {6} self-
// animation (Duration$ Permanent, Power$ 3 | Toughness$ 3, types+P/T halves).
// The grant exists while the land stays in its zone (including through the
// turn-1 end-of-turn cleanup, the control), ends on the departure move, and
// does NOT re-apply on re-entry.
func TestPermanentAnimateGrantEndsOnZoneChange(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := corpusEngineCfg(t, reg, []*cards.Card{lookup(t, reg, "Stalking Stones")}, []*cards.Card{})
	id := moveByName(t, e, 0, "Stalking Stones", state.ZBattlefield)

	addMana(t, e, 0, "CCCCCC")
	submitChoices(t, e, animateAbilityOption(t, e, id).Index)
	settleActivation(t, e)

	d := e.Derived(id)
	if d.Power != 3 || d.Toughness != 3 {
		t.Fatalf("precondition: animated Stalking Stones = %d/%d, want 3/3", d.Power, d.Toughness)
	}
	if !e.IsCreature(id) {
		t.Fatal("precondition: animated Stalking Stones is not a creature")
	}

	// Control: the grant survives the turn-1 end-of-turn cleanup while the
	// object never leaves its zone (CR 611.2a's indefinite duration).
	driveToStep(t, e, 2, 1, state.StepMain1)
	d = e.Derived(id)
	if d.Power != 3 || d.Toughness != 3 || !e.IsCreature(id) {
		t.Fatalf("control: the permanent animation did not survive in-zone cleanup: %d/%d creature=%v",
			d.Power, d.Toughness, e.IsCreature(id))
	}

	// Departure: the animated land bounces to its owner's hand.
	e.emit(events.Event{Kind: events.MoveZone, Obj: id,
		From: state.ZBattlefield, To: state.ZHand})
	d = e.Derived(id)
	if e.IsCreature(id) {
		t.Fatal("the animation grant survived the object's zone change (CR 400.7 violation)")
	}
	if d.Power == 3 || d.Toughness == 3 {
		t.Fatalf("the P/T grant survived the zone change: %d/%d", d.Power, d.Toughness)
	}

	// Re-entry: the same id returns to the battlefield a plain land.
	e.emit(events.Event{Kind: events.MoveZone, Obj: id,
		From: state.ZHand, To: state.ZBattlefield})
	d = e.Derived(id)
	if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: object did not return to the battlefield: %+v", o)
	}
	if e.IsCreature(id) {
		t.Fatal("the animation grant re-applied on re-entry")
	}
	if d.Power == 3 || d.Toughness == 3 {
		t.Fatalf("the P/T grant re-applied on re-entry: %d/%d", d.Power, d.Toughness)
	}
	replayCheck(t, e, cfg)
}

// TestPermanentPumpGrantEndsOnZoneChange: Mist Dragon's {0} self-pump
// (AB$ Pump | Cost$ 0 | Defined$ Self | KW$ Flying | Duration$ Permanent).
// The keyword grant exists before departure (and through the turn-1 cleanup,
// the control), ends on the departure move, and does not re-apply on
// re-entry. The printed ability itself stays -- only the swept grant is gone.
func TestPermanentPumpGrantEndsOnZoneChange(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := corpusEngineCfg(t, reg, []*cards.Card{lookup(t, reg, "Mist Dragon")}, []*cards.Card{})
	id := moveByName(t, e, 0, "Mist Dragon", state.ZBattlefield)

	if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Mist Dragon not on the battlefield: %+v", o)
	}
	if e.HasKeyword(id, "Flying") {
		t.Fatal("precondition: Mist Dragon already has flying; the grant would be indistinguishable")
	}
	// The face's AB$ Pump ability is what gets activated -- its presence is
	// the "the handler ran" anchor: if the grant later vanishes it is because
	// the sweep ended it, not because nothing was ever registered.
	pumpIdx := -1
	for i, sa := range e.G.Obj(id).Face().Abilities {
		if sa.Kind == "AB" && sa.API == "Pump" {
			pumpIdx = i
			break
		}
	}
	if pumpIdx < 0 {
		t.Fatal("precondition: Mist Dragon carries no AB$ Pump ability")
	}
	// The pending decision is stale after moveByName (addMana is what re-asks
	// priority in the Animate test; a Cost$ 0 activation has no pool to fund),
	// so re-ask explicitly -- the same shape the Karn +1 test uses.
	e.pending = nil
	e.priorityRound()
	submitChoices(t, e, abilityOption(t, e, id, pumpIdx).Index)
	settleActivation(t, e)

	if !e.HasKeyword(id, "Flying") {
		t.Fatal("precondition: the permanent flying grant did not apply")
	}

	// Control: the grant survives the turn-1 end-of-turn cleanup in-zone.
	driveToStep(t, e, 2, 1, state.StepMain1)
	if !e.HasKeyword(id, "Flying") {
		t.Fatal("control: the permanent pump grant did not survive in-zone cleanup")
	}

	// Departure and re-entry.
	e.emit(events.Event{Kind: events.MoveZone, Obj: id,
		From: state.ZBattlefield, To: state.ZHand})
	if e.HasKeyword(id, "Flying") {
		t.Fatal("the pump grant survived the object's zone change (CR 400.7 violation)")
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: id,
		From: state.ZHand, To: state.ZBattlefield})
	if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: object did not return to the battlefield: %+v", o)
	}
	if e.HasKeyword(id, "Flying") {
		t.Fatal("the pump grant re-applied on re-entry")
	}
	replayCheck(t, e, cfg)
}
