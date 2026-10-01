package rules

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestNahiriForgedInFuryMayPlayEquipmentValidAfterStack is the real-card
// regression for the Effect-delivered MayPlay ValidAfterStack$ filter (Nahiri,
// Forged in Fury). Her attack trigger exiles the top card and registers two
// Effect-delivered grants over it: STPlay (MayPlay$ True, Affected$
// Card.IsRemembered) and STPlay2 (MayPlay$ True, MayPlayWithoutManaCost$ True,
// Affected$ Equipment.IsRemembered, ValidAfterStack$ Spell.Equipment). Before
// the fix the shared effect-registration whitelist rejected the ValidAfterStack$
// key, so STPlay2 never registered and the free Equipment cast was never
// offered.
//
// The discriminator is affordability with an EMPTY mana pool: both exiled
// cards cost mana, so only the FREE grant can make the Equipment castable.
// STPlay alone offers both cards but requires their printed cost, and an
// unaffordable offer is not emitted.
func TestNahiriForgedInFuryMayPlayEquipmentValidAfterStack(t *testing.T) {
	e := combatEngine(t)
	e.G.Players[0].Pool = state.Mana{}
	nahiri := onBoardCard(t, e, 0, mayPlayAfterStackCard(t, "Nahiri, Forged in Fury"))
	attacker := onBoardReady(t, e, 0, "Name:Nahiri Test Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	// A second Equipment on the battlefield, attached to the attacker, is what
	// makes `Creature.equipped+YouCtrl` match Nahiri's trigger.
	onBoardEquip := onBoardCard(t, e, 0, mayPlayAfterStackCard(t, "Darksteel Axe"))
	e.emit(events.Event{Kind: events.Attach, Obj: onBoardEquip, IDs: []state.ObjID{attacker}})

	// The exiled pair: the top card is an Equipment (free-castable), the next
	// is a non-Equipment spell. Both cost mana, so the empty pool is what
	// separates the free grant from the ordinary one.
	equipmentCard := mayPlayAfterStackCard(t, "Bonesplitter")
	nonEquipmentCard := mayPlayAfterStackCard(t, "Grizzly Bears")
	equipment := e.G.AddObject(equipmentCard, 0)
	nonEquipment := e.G.AddObject(nonEquipmentCard, 0)
	e.G.SetZone(state.ZLibrary, 0, []state.ObjID{equipment.ID, nonEquipment.ID})

	if e.G.Obj(nahiri).Zone != state.ZBattlefield || e.G.Obj(onBoardEquip).Zone != state.ZBattlefield ||
		e.G.Obj(onBoardEquip).AttachedTo != attacker {
		t.Fatalf("precondition: Nahiri=%v equipment=%v attachedTo=%d, want battlefield and attached to attacker %d",
			e.G.Obj(nahiri).Zone, e.G.Obj(onBoardEquip).Zone, e.G.Obj(onBoardEquip).AttachedTo, attacker)
	}
	if !slices.Contains(equipmentCard.Faces[0].Types, "Equipment") || slices.Contains(nonEquipmentCard.Faces[0].Types, "Equipment") {
		t.Fatal("precondition: Bonesplitter must be an Equipment and Grizzly Bears must not")
	}
	if equipmentCard.Faces[0].ManaValue() == 0 || nonEquipmentCard.Faces[0].ManaValue() == 0 {
		t.Fatal("precondition: both exiled cards must cost mana so the empty pool is the discriminator")
	}
	if equipment.Zone != state.ZLibrary || nonEquipment.Zone != state.ZLibrary ||
		e.G.Zone(state.ZLibrary, 0)[0] != equipment.ID {
		t.Fatal("precondition: library must hold the Equipment on top followed by the non-Equipment")
	}

	// Two attacks: each exiles the current library top (Equipment, then
	// non-Equipment) and leaves an UntilEOT grant remembering it.
	for i := 0; i < 2; i++ {
		e.emit(events.Event{Kind: events.DeclareAttackers, Player: 0, IDs: []state.ObjID{attacker}})
		e.putTriggersOnStack()
		if len(e.G.Stack) == 0 {
			t.Fatalf("Nahiri attack %d did not queue a trigger", i+1)
		}
		e.resolveTop()
		passUntilStackEmpty(t, e, 20)
	}
	if equipment.Zone != state.ZExile || nonEquipment.Zone != state.ZExile {
		t.Fatalf("precondition: equipment zone=%s non-equipment zone=%s, want both exiled",
			equipment.Zone, nonEquipment.Zone)
	}
	// The grants really registered, and STPlay2 carried the qualifier.
	freeGrant := false
	for _, ce := range e.active() {
		if ce.MayPlay && ce.MayPlayValidAfterStack == "Spell.Equipment" {
			freeGrant = true
		}
	}
	if !freeGrant {
		t.Fatal("precondition: no active Effect-delivered grant carries ValidAfterStack$ Spell.Equipment")
	}

	// The offer walk. An empty pool plus a sorcery-speed window: only a FREE
	// grant can make either card affordable.
	e.G.Step, e.G.Active, e.G.Priority, e.G.Turn = state.StepMain1, 0, 0, 1
	e.pending = nil
	e.Advance()
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("priority = %+v, want a priority decision", d)
	}
	offers := func(id state.ObjID) bool {
		for _, o := range d.Options {
			if o.Kind == "cast" && o.Mode == "mayplay" && o.Obj == id {
				return true
			}
		}
		return false
	}
	if !offers(equipment.ID) {
		t.Fatalf("free Equipment mayplay offer missing (empty pool, STPlay2 must supply the free grant): %+v", d.Options)
	}
	if offers(nonEquipment.ID) {
		t.Fatalf("non-Equipment spell wrongly offered (empty pool, printed cost unaffordable): %+v", d.Options)
	}
	// The classification behind the offers: the Equipment is a FREE grant
	// (STPlay2), the non-Equipment only a plain permission (STPlay). This is
	// the ValidAfterStack$/free distinction, read through the same predicates
	// the offer walk uses, rather than an incidental Affected$ exclusion.
	if free, covered := e.mayPlayEffectFree(0, e.G.Obj(equipment.ID)); !free || !covered {
		t.Fatalf("mayPlayEffectFree(equipment) = free %v covered %v, want free true covered true", free, covered)
	}
	if free, covered := e.mayPlayEffectFree(0, e.G.Obj(nonEquipment.ID)); free || covered {
		t.Fatalf("mayPlayEffectFree(non-equipment) = free %v covered %v, want free false covered false", free, covered)
	}
	if !e.mayPlayEffectGrantsCast(0, e.G.Obj(nonEquipment.ID)) {
		t.Fatal("non-equipment not covered by the plain STPlay grant")
	}
	if free, ok := e.mayPlayGrant(0, equipment.ID); !free || !ok {
		t.Fatalf("mayPlayGrant(equipment) = free %v ok %v, want free true ok true", free, ok)
	}
}

// TestEffectGrantValidAfterStackGateFailsClosed pins the consumption half:
// effectGrantMatches evaluates a grant's carried ValidAfterStack$ against the
// card with the derived stack view, admits a matching spell characteristic and
// rejects both a nonmatching one and an unsupported predicate value. The
// grant's Affected$ is broadened to Card.IsRemembered so the qualifier is the
// ONLY thing distinguishing the two cards.
func TestEffectGrantValidAfterStackGateFailsClosed(t *testing.T) {
	e := combatEngine(t)
	equipmentCard := mayPlayAfterStackCard(t, "Bonesplitter")
	nonEquipmentCard := mayPlayAfterStackCard(t, "Grizzly Bears")
	equipment := e.G.AddObject(equipmentCard, 0)
	equipment.Zone = state.ZExile
	nonEquipment := e.G.AddObject(nonEquipmentCard, 0)
	nonEquipment.Zone = state.ZExile
	e.G.SetZone(state.ZExile, 0, []state.ObjID{equipment.ID, nonEquipment.ID})

	if !slices.Contains(equipmentCard.Faces[0].Types, "Equipment") || slices.Contains(nonEquipmentCard.Faces[0].Types, "Equipment") {
		t.Fatal("precondition: Bonesplitter must be an Equipment and Grizzly Bears must not")
	}
	if equipment.Zone != state.ZExile || nonEquipment.Zone != state.ZExile {
		t.Fatal("precondition: both cards must sit in exile")
	}

	grant := state.ContinuousEffect{
		Source:                 equipment.ID,
		Controller:             0,
		Affects:                "Card.IsRemembered",
		MayPlay:                true,
		MayPlayValidAfterStack: "Spell.Equipment",
		Remembered:             []state.ObjID{equipment.ID, nonEquipment.ID},
	}
	if !e.effectGrantMatches(&grant, equipment.ID) {
		t.Fatal("Equipment card rejected by its own Spell.Equipment qualifier")
	}
	if e.effectGrantMatches(&grant, nonEquipment.ID) {
		t.Fatal("non-Equipment card passed the Spell.Equipment qualifier")
	}
	// An unsupported predicate value must remain fail-closed.
	grant.MayPlayValidAfterStack = "Spell.NotAPredicate"
	if e.effectGrantMatches(&grant, equipment.ID) {
		t.Fatal("an unsupported ValidAfterStack$ value was accepted (must fail closed)")
	}
}
