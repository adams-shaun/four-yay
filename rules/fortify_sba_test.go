package rules

// The CR 704.5n Fortification half of the attachment SBAs: a Fortification
// whose bearer is no longer a land detaches and stays on the battlefield
// (CR 702.67b), while one whose bearer merely GAINED a type (an animated
// manland-shaped static) stays attached. The land test reads the DERIVED type
// list, so the bearer here loses its Land type to a layer-4
// RemoveCardTypes$ static rather than being printed without one. Everything
// moves through logged MoveZone events (moveSeeded, not putLands) so
// replayCheck rebuilds the exact game.

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

const fortifySBABearer = "Name:Fortstone\nManaCost:2\nTypes:Artifact Fortification\nK:Fortify:3\n" +
	"T:Mode$ TapsForMana | ValidCard$ Card.FortifiedBy | Execute$ TrigPutCounter | TriggerDescription$ x\n" +
	"SVar:TrigPutCounter:DB$ PutCounter | ValidTgts$ Creature.YouCtrl | CounterType$ P1P1\nOracle:x\n"

const fortifySBALand = "Name:Fortified Hill\nManaCost:0\nTypes:Basic Land Mountain\nOracle:x\n"

const fortifySBABear = "Name:Fortstone Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"

// The Kenrith's Transformation / Darksteel Mutation shape (every corpus line
// carrying RemoveCardTypes$ also carries AddType$, and the layer-4 emission
// gates on that): the affected land becomes a Construct, losing the Land card
// type and its Mountain subtype (a subtype is tied to its card type) while
// keeping the Basic supertype.
const fortifySBALandStripper = "Name:TypeStripper\nManaCost:2 U\nTypes:Enchantment\n" +
	"S:Mode$ Continuous | Affected$ Land.YouCtrl | AddTypes$ Construct | RemoveCardTypes$ True | Description$ x\nOracle:x\n"

// The manland shape: adds Creature AND sets a real P/T. A bare
// `AddTypes$ Creature` leaves the land a 0/0, which the CR 704.5f SBA now
// (correctly) destroys -- that would detach the Fortification because the
// bearer left the battlefield, not because it stopped being a land, and the
// point of the test below is the latter. The SetPower/SetToughness pair keeps
// the animated bearer alive so the Fortification SBA is the only thing under
// test.
const fortifySBAAnimator = "Name:LandAnimator\nManaCost:2 R\nTypes:Enchantment\n" +
	"S:Mode$ Continuous | Affected$ Land.YouCtrl | AddTypes$ Creature | SetPower$ 3 | SetToughness$ 3 | Description$ x\nOracle:x\n"

func TestFortificationDetachesWhenBearerStopsBeingALand(t *testing.T) {
	t.Parallel()
	e, cfg, fort := newFixtureDeck(t, 91, fortifySBABearer, fortifySBALand, fortifySBABear, fortifySBALandStripper)
	e.emit(events.Event{Kind: events.MoveZone, Obj: fort, From: state.ZHand, To: state.ZBattlefield})
	land := moveSeeded(t, e, 0, fortifySBALand, state.ZBattlefield)
	bear := moveSeeded(t, e, 0, fortifySBABear, state.ZBattlefield)
	addMana(t, e, 0, "CCC")
	e.Advance()
	opt := abilityOption(t, e, fort, 0)
	submitChoices(t, e, opt.Index)
	targetObject(t, e, land)
	passUntilStackEmpty(t, e, 20)
	if e.G.Obj(fort).AttachedTo != land {
		t.Fatalf("precondition: Fortstone attached to %d, want the land %d", e.G.Obj(fort).AttachedTo, land)
	}

	// While attached, the fortified land's tap fires the Fortification's own
	// TapsForMana trigger through Card.FortifiedBy (the feature works before
	// the bearer changes).
	e.resolveManaAbility(0, land, e.availableManaAbilities(0, land)[0], false)
	if len(e.pendingTriggers) != 1 {
		t.Fatalf("tapping the fortified land queued %d triggers, want one", len(e.pendingTriggers))
	}
	e.putTriggersOnStack()
	targetObject(t, e, bear)
	passUntilStackEmpty(t, e, 30)
	if got := e.G.Obj(bear).Counter("P1P1"); got != 1 {
		t.Fatalf("precondition: the TapsForMana trigger put %d counters, want 1", got)
	}

	// Strip the Land card type with a layer-4 static. First assert the strip
	// is actually live in the derived types, so the SBA assertion below is
	// not vacuous.
	moveSeeded(t, e, 0, fortifySBALandStripper, state.ZBattlefield)
	landStill := false
	for _, typ := range e.Derived(land).Types {
		if typ == "Land" {
			landStill = true
		}
	}
	if landStill {
		t.Fatalf("precondition: derived types after the strip still carry Land: %v", e.Derived(land).Types)
	}
	if e.G.Obj(fort).AttachedTo != land {
		t.Fatal("precondition: the Fortification must still be attached before the SBA runs")
	}

	e.checkStateBased()
	if e.G.Obj(fort).Zone != state.ZBattlefield {
		t.Fatalf("Fortstone left the battlefield for %s, want detach-and-stay", e.G.Obj(fort).Zone)
	}
	if e.G.Obj(fort).AttachedTo != 0 {
		t.Fatalf("Fortstone still attached to %d after the bearer lost its Land type", e.G.Obj(fort).AttachedTo)
	}
	detached := false
	for _, ev := range e.L.Events {
		if ev.Kind == events.Unattached && ev.Obj == fort && len(ev.IDs) == 1 && ev.IDs[0] == land &&
			ev.Text == "Fortification bearer is no longer a land" {
			detached = true
		}
	}
	if !detached {
		t.Fatal("no Unattached event naming the former land bearer")
	}

	// No fortified-land effects after the detach: the same tap now queues
	// nothing, because Card.FortifiedBy reads the zeroed attach relation.
	e.emit(events.Event{Kind: events.Untap, Obj: land})
	e.resolveManaAbility(0, land, e.availableManaAbilities(0, land)[0], false)
	if len(e.pendingTriggers) != 0 {
		t.Fatalf("tapping the (former) bearer after the detach queued %d triggers, want none", len(e.pendingTriggers))
	}
	replayCheck(t, e, cfg)
}

func TestFortifiedLandThatGainsATypeStaysAttached(t *testing.T) {
	t.Parallel()
	e, cfg, fort := newFixtureDeck(t, 92, fortifySBABearer, fortifySBALand, fortifySBAAnimator)
	e.emit(events.Event{Kind: events.MoveZone, Obj: fort, From: state.ZHand, To: state.ZBattlefield})
	land := moveSeeded(t, e, 0, fortifySBALand, state.ZBattlefield)
	addMana(t, e, 0, "CCC")
	e.Advance()
	opt := abilityOption(t, e, fort, 0)
	submitChoices(t, e, opt.Index)
	targetObject(t, e, land)
	passUntilStackEmpty(t, e, 20)
	if e.G.Obj(fort).AttachedTo != land {
		t.Fatalf("precondition: Fortstone attached to %d, want the land %d", e.G.Obj(fort).AttachedTo, land)
	}

	// A manland-shaped layer-4 grant adds Creature without removing Land:
	// the bearer is still a land, so the SBA must not detach.
	moveSeeded(t, e, 0, fortifySBAAnimator, state.ZBattlefield)
	derived := e.Derived(land).Types
	hasCreature, hasLand := false, false
	for _, typ := range derived {
		if typ == "Creature" {
			hasCreature = true
		}
		if typ == "Land" {
			hasLand = true
		}
	}
	if !hasCreature || !hasLand {
		t.Fatalf("precondition: derived types after the grant = %v, want Creature and Land", derived)
	}
	// Precondition for the SBA assertion below: the animated bearer must be
	// alive (a real 3/3), otherwise it dies to CR 704.5f and the Fortification
	// detaches for the wrong reason.
	if d := e.Derived(land); d.Power != 3 || d.Toughness != 3 {
		t.Fatalf("precondition: animated bearer = %d/%d, want 3/3", d.Power, d.Toughness)
	}
	e.checkStateBased()
	if e.G.Obj(land).Zone != state.ZBattlefield {
		t.Fatalf("the animated 3/3 bearer left the battlefield for %s; the 704.5f sweep must not run", e.G.Obj(land).Zone)
	}
	if e.G.Obj(fort).AttachedTo != land || e.G.Obj(fort).Zone != state.ZBattlefield {
		t.Fatalf("Fortstone after an animated bearer: attached %d in %s, want still attached to %d",
			e.G.Obj(fort).AttachedTo, e.G.Obj(fort).Zone, land)
	}
	for _, ev := range e.L.Events {
		if ev.Kind == events.Unattached && ev.Obj == fort {
			t.Fatalf("an animated (still-land) bearer must not detach: %+v", ev)
		}
	}
	replayCheck(t, e, cfg)
}
