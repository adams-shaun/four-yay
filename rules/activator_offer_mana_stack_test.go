package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// hasActivateOptionFor reports whether options offers the mana "activate"
// (tap-for-mana) option for obj -- the Kind the mana-ability offer walk
// (legalActionsPriced) appends, distinct from the non-mana "ability" Kind
// hasAbilityOptionFor looks for.
func hasActivateOptionFor(options []decision.Option, id state.ObjID) bool {
	for _, o := range options {
		if o.Kind == "activate" && o.Obj == id {
			return true
		}
	}
	return false
}

// TestActivatorOfferManaAbility covers the mana-ability half of the
// Activator$ offer gate. A mana ability is still an activation: its
// Activator$ decides who may tap it, and the mana offer walk must enumerate
// the sources another player's selector admits rather than only the asking
// seat's own battlefield. The fixture is an opponent-only mana source
// controlled by seat 0: the controller must NOT see it, the opponent must.
func TestActivatorOfferManaAbility(t *testing.T) {
	const src = `Name:Activator Mana Probe
ManaCost:0
Types:Artifact
A:AB$ Mana | Cost$ T | Produced$ C | Activator$ Player.Opponent | SpellDescription$ Add {C}. Only your opponents may activate this ability.
Oracle:x
`
	e, _, id := newFixtureDeck(t, 8611, src)
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZBattlefield})
	driveToStep(t, e, 1, 0, state.StepMain1)
	if got := e.controllerOf(id); got != 0 || e.G.Obj(id).Zone != state.ZBattlefield {
		t.Fatalf("fixture precondition: controller=%d zone=%s", got, e.G.Obj(id).Zone)
	}
	if e.G.Obj(id).Tapped {
		t.Fatal("fixture precondition: mana source is already tapped")
	}
	if hasActivateOptionFor(e.legalActions(0), id) {
		t.Fatal("opponent-only mana ability was offered to its controller")
	}

	// On the opponent's own main phase the same source's mana ability must be
	// offered to them, not merely withheld from the controller.
	driveToStep(t, e, 2, 1, state.StepMain1)
	if e.G.Active != 1 {
		t.Fatalf("fixture precondition: active=%d", e.G.Active)
	}
	if got := e.controllerOf(id); got != 0 || e.G.Obj(id).Zone != state.ZBattlefield {
		t.Fatalf("fixture changed before opponent offer: controller=%d zone=%s", got, e.G.Obj(id).Zone)
	}
	if e.G.Obj(id).Tapped {
		t.Fatal("fixture precondition: mana source is tapped before the opponent offer")
	}
	if !hasActivateOptionFor(e.legalActions(1), id) {
		t.Fatalf("opponent was not offered the mana ability: %+v", e.legalActions(1))
	}
}

// TestActivatorOfferManaAbilityAnyPlayer covers the Mana Cache shape
// (Activator$ Player, "Any player may activate this ability"): both the
// controller and the opponent are offered the source, so the gate admits
// rather than only narrows.
func TestActivatorOfferManaAbilityAnyPlayer(t *testing.T) {
	const src = `Name:Activator Any-Mana Probe
ManaCost:0
Types:Artifact
A:AB$ Mana | Cost$ T | Produced$ C | Activator$ Player | SpellDescription$ Add {C}. Any player may activate this ability.
Oracle:x
`
	e, _, id := newFixtureDeck(t, 8612, src)
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZBattlefield})
	driveToStep(t, e, 1, 0, state.StepMain1)
	if got := e.controllerOf(id); got != 0 || e.G.Obj(id).Zone != state.ZBattlefield {
		t.Fatalf("fixture precondition: controller=%d zone=%s", got, e.G.Obj(id).Zone)
	}
	if !hasActivateOptionFor(e.legalActions(0), id) {
		t.Fatalf("any-player mana ability was not offered to its controller: %+v", e.legalActions(0))
	}
	if !hasActivateOptionFor(e.legalActions(1), id) {
		t.Fatalf("any-player mana ability was not offered to the opponent: %+v", e.legalActions(1))
	}
}

// TestActivatorOfferStackAbility covers the Lightning Storm shape
// (ActivationZone$ Stack, Activator$ Player): a printed AB$ whose source
// lives on the stack must be offered through the stack walk to the player a
// Player selector admits, not silently skipped because the source is not on
// a battlefield.
func TestActivatorOfferStackAbility(t *testing.T) {
	const src = `Name:Activator Stack Probe
ManaCost:2 R
Types:Instant
A:AB$ PutCounter | Cost$ 0 | CounterType$ CHARGE | CounterNum$ 2 | Defined$ Self | ActivationZone$ Stack | Activator$ Player | SpellDescription$ Put two charge counters on CARDNAME. Any player may activate this ability but only if CARDNAME is on the stack.
Oracle:x
`
	e, _, id := newFixtureDeck(t, 8613, src)
	// Drive to a main phase FIRST, then stack the card: driving afterwards
	// would let the instant resolve off the stack before the offer is read.
	driveToStep(t, e, 1, 0, state.StepMain1)
	e.emit(events.Event{Kind: events.PutOnStack, Obj: id, Player: 0, From: state.ZHand, To: state.ZStack})
	if got := e.G.Obj(id).Zone; got != state.ZStack {
		t.Fatalf("fixture precondition: object zone=%s, want stack", got)
	}
	if e.G.Obj(id).Face() == nil || !faceHasActivationZone(e.G.Obj(id).Face(), "Stack") {
		t.Fatal("fixture precondition: stacked object does not carry an ActivationZone$ Stack ability")
	}
	// legalActions is a pure per-seat query (independent of whose priority is
	// pending), so both seats' offers are read against the one stack state.
	if !hasAbilityOptionFor(e.legalActions(0), id) {
		t.Fatalf("stack ability was not offered to its controller: %+v", e.legalActions(0))
	}
	if !hasAbilityOptionFor(e.legalActions(1), id) {
		t.Fatalf("stack ability was not offered to the opponent: %+v", e.legalActions(1))
	}
}
