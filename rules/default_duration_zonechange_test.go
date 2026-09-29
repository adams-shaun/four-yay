package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// A self-Animate with no Duration$ is until end of turn and belongs to the
// particular permanent incarnation that activated it (CR 400.7).
func TestDefaultDurationAnimateEndsOnZoneChange(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := corpusEngineCfg(t, reg, []*cards.Card{lookup(t, reg, "Blinkmoth Nexus")}, []*cards.Card{})
	id := moveByName(t, e, 0, "Blinkmoth Nexus", state.ZBattlefield)
	if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield || e.IsCreature(id) {
		t.Fatalf("precondition: Blinkmoth Nexus must start as a noncreature battlefield land: %+v", o)
	}
	addMana(t, e, 0, "C")
	submitChoices(t, e, animateAbilityOption(t, e, id).Index)
	settleActivation(t, e)
	if !e.IsCreature(id) {
		t.Fatal("precondition: default-duration Animate did not animate Blinkmoth Nexus")
	}

	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZBattlefield, To: state.ZExile})
	if o := e.G.Obj(id); o == nil || o.Zone != state.ZExile {
		t.Fatalf("precondition: animated land did not leave the battlefield: %+v", o)
	}
	if e.IsCreature(id) {
		t.Fatal("default-duration Animate survived the departure MoveZone")
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZExile, To: state.ZBattlefield})
	if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: land did not return to the battlefield: %+v", o)
	}
	if e.IsCreature(id) {
		t.Fatal("default-duration Animate re-applied to the returned incarnation")
	}
	replayCheck(t, e, cfg)
}

// A self-Pump with no Duration$ likewise ends with the battlefield
// incarnation that activated it, rather than following the stable ObjID.
func TestDefaultDurationPumpEndsOnZoneChange(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := corpusEngineCfg(t, reg, []*cards.Card{lookup(t, reg, "Killer Bees")}, []*cards.Card{})
	id := moveByName(t, e, 0, "Killer Bees", state.ZBattlefield)
	if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Killer Bees not on the battlefield: %+v", o)
	}
	before := e.Derived(id)
	pumpIndex := -1
	for i, sa := range e.G.Obj(id).Face().Abilities {
		if sa.Kind == "AB" && sa.API == "Pump" {
			pumpIndex = i
			break
		}
	}
	if pumpIndex < 0 {
		t.Fatal("precondition: Killer Bees carries no AB$ Pump")
	}
	addMana(t, e, 0, "G")
	submitChoices(t, e, abilityOption(t, e, id, pumpIndex).Index)
	settleActivation(t, e)
	after := e.Derived(id)
	if after.Power == before.Power || after.Toughness == before.Toughness {
		t.Fatalf("precondition: no-op pump; before=%d/%d after=%d/%d", before.Power, before.Toughness, after.Power, after.Toughness)
	}

	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZBattlefield, To: state.ZExile})
	if o := e.G.Obj(id); o == nil || o.Zone != state.ZExile {
		t.Fatalf("precondition: pumped creature did not leave the battlefield: %+v", o)
	}
	departed := e.Derived(id)
	if departed.Power == after.Power && departed.Toughness == after.Toughness {
		t.Fatalf("default-duration Pump survived the departure MoveZone: got %d/%d, granted %d/%d", departed.Power, departed.Toughness, after.Power, after.Toughness)
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZExile, To: state.ZBattlefield})
	if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: creature did not return to the battlefield: %+v", o)
	}
	returned := e.Derived(id)
	if returned.Power != before.Power || returned.Toughness != before.Toughness {
		t.Fatalf("default-duration Pump re-applied to returned incarnation: got %d/%d, want unpumped %d/%d", returned.Power, returned.Toughness, before.Power, before.Toughness)
	}
	replayCheck(t, e, cfg)
}
