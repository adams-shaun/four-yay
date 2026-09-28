package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// fx20TriggerWithParam finds the face trigger on id one of whose parameter
// values contains want, so a rule is tested against the REAL corpus script
// rather than a hand-built trigger.
func fx20TriggerWithParam(t *testing.T, e *Engine, id state.ObjID, want string) cards.Trigger {
	t.Helper()
	o := e.G.Obj(id)
	if o == nil || o.Face() == nil {
		t.Fatalf("fx20TriggerWithParam: no face for %d", id)
	}
	for _, tr := range o.Face().Triggers {
		for _, v := range tr.Params {
			if strings.Contains(v, want) {
				return tr
			}
		}
	}
	t.Fatalf("no trigger carrying %q on %s", want, o.Face().Name)
	return cards.Trigger{}
}

// fx20BattlefieldByName moves the named seat-p card from its library or hand
// to the battlefield through a logged MoveZone and returns its id.
func fx20BattlefieldByName(t *testing.T, e *Engine, p state.PlayerID, name string) state.ObjID {
	t.Helper()
	for _, z := range []state.Zone{state.ZLibrary, state.ZHand} {
		for _, id := range e.G.Zone(z, p) {
			if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == name {
				e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: z, To: state.ZBattlefield})
				e.pending = nil
				return id
			}
		}
	}
	t.Fatalf("card %q not in seat %d's library or hand", name, p)
	return 0
}

// TestPlayerSpecFx20MindWhipEnchantedController drives the REAL corpus Aura
// Mind Whip's upkeep trigger ("At the beginning of the upkeep of enchanted
// creature's controller ...") through phaseMatches. The aura belongs to seat
// 0 and enchants seat 1's creature, so Player.EnchantedController must name
// seat 1 -- a fallback to the source's own controller would name seat 0.
func TestPlayerSpecFx20MindWhipEnchantedController(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, _, ids := dsBoardWith(t, reg, mustCorpusCard(t, reg, "Grizzly Bears"), "Mind Whip")
	aura := ids["Mind Whip"]
	bear := fx20BattlefieldByName(t, e, 1, "Grizzly Bears")
	if e.G.Obj(aura).Zone != state.ZBattlefield || e.G.Obj(bear).Zone != state.ZBattlefield {
		t.Fatal("precondition: both the aura and the bearer must be on the battlefield")
	}
	if e.controllerOf(aura) != 0 || e.controllerOf(bear) != 1 {
		t.Fatalf("precondition: aura ctrl = %d, bearer ctrl = %d; want 0 and 1",
			e.controllerOf(aura), e.controllerOf(bear))
	}
	e.emit(events.Event{Kind: events.Attach, Obj: aura, IDs: []state.ObjID{bear}})
	if e.G.Obj(aura).AttachedTo != bear {
		t.Fatal("precondition: the aura did not attach to the bear")
	}
	trig := fx20TriggerWithParam(t, e, aura, "Player.EnchantedController")

	e.G.Active = 1
	if !e.phaseMatches(trig, aura, events.Event{Kind: events.StepChange}) {
		t.Fatal("the enchanted creature's controller's upkeep must fire the trigger")
	}
	e.G.Active = 0
	if e.phaseMatches(trig, aura, events.Event{Kind: events.StepChange}) {
		t.Fatal("seat 0's upkeep must not fire the trigger")
	}
}

// TestPlayerSpecFx20MissHighwaterPlayerCounters drives the REAL corpus card
// Miss Highwater's "deals combat damage to a player who doesn't have a
// contract counter" trigger through damageMatches. The read must be the
// recipient's live player-counter state: seat 1 with no contract counter
// matches, and adding one flips the answer.
func TestPlayerSpecFx20MissHighwaterPlayerCounters(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, _, ids := dsBoard(t, reg, "Miss Highwater")
	mhw := ids["Miss Highwater"]
	if e.G.Obj(mhw).Zone != state.ZBattlefield || e.controllerOf(mhw) != 0 {
		t.Fatal("precondition: Miss Highwater must be on seat 0's battlefield")
	}
	if n := e.G.Players[1].Counter("Contract"); n != 0 {
		t.Fatalf("precondition: seat 1 already has %d contract counters", n)
	}
	trig := fx20TriggerWithParam(t, e, mhw, "Player.counters_EQ0_Contract")
	e.combatDamaging = true
	e.dmgSrcOverride = mhw

	if !e.damageMatches(trig, mhw, events.Event{Kind: events.Damage, Player: 1}) {
		t.Fatal("combat damage to a player with no contract counter must match")
	}
	e.G.Players[1].AddCounter("Contract", 1)
	if e.damageMatches(trig, mhw, events.Event{Kind: events.Damage, Player: 1}) {
		t.Fatal("combat damage to a player WITH a contract counter must not match")
	}
}

// TestPlayerSpecFx20LatullasOrdersDefendingPlayer drives the REAL corpus Aura
// Latulla's Orders' "enchanted creature deals combat damage to defending
// player" trigger through damageMatches: the Damage event's recipient is the
// defending player the clause names, and the attached bearer is the damage
// source the ValidSource$ half requires.
func TestPlayerSpecFx20LatullasOrdersDefendingPlayer(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, _, ids := dsBoardWith(t, reg, mustCorpusCard(t, reg, "Grizzly Bears"), "Latulla's Orders")
	aura := ids["Latulla's Orders"]
	bear := fx20BattlefieldByName(t, e, 1, "Grizzly Bears")
	e.emit(events.Event{Kind: events.Attach, Obj: aura, IDs: []state.ObjID{bear}})
	if e.G.Obj(aura).AttachedTo != bear || e.controllerOf(bear) != 1 {
		t.Fatalf("precondition: aura attached %d, bearer ctrl %d; want %d and 1",
			e.G.Obj(aura).AttachedTo, e.controllerOf(bear), bear)
	}
	trig := fx20TriggerWithParam(t, e, aura, "Player.TriggeredDefendingPlayer")
	e.combatDamaging = true
	e.dmgSrcOverride = bear

	if !e.damageMatches(trig, aura, events.Event{Kind: events.Damage, Player: 1}) {
		t.Fatal("combat damage to seat 1 must read seat 1 as the defending player")
	}
}

// TestPlayerSpecFx20BrandCasterEnchantedController drives the REAL corpus Aura
// Brand of Ill Omen's "enchanted creature's controller can't cast creature
// spells" static through actorMatches, the Caster$/Activator$ call site: the
// scoped caster is the controller of the enchanted creature (seat 1), not the
// aura's own controller (seat 0).
func TestPlayerSpecFx20BrandCasterEnchantedController(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, _, ids := dsBoardWith(t, reg, mustCorpusCard(t, reg, "Grizzly Bears"), "Brand of Ill Omen")
	aura := ids["Brand of Ill Omen"]
	bear := fx20BattlefieldByName(t, e, 1, "Grizzly Bears")
	e.emit(events.Event{Kind: events.Attach, Obj: aura, IDs: []state.ObjID{bear}})
	if e.G.Obj(aura).AttachedTo != bear || e.controllerOf(bear) != 1 || e.controllerOf(aura) != 0 {
		t.Fatal("precondition: aura must be attached to seat 1's creature and controlled by seat 0")
	}
	var sv staticView
	found := false
	for _, st := range e.G.Obj(aura).Face().Statics {
		if st.Params["Caster"] == "Player.EnchantedController" {
			sv = staticView{Source: aura, Controller: 0, Params: st.Params}
			found = true
		}
	}
	if !found {
		t.Fatal("precondition: Brand of Ill Omen carries no Caster$ Player.EnchantedController static")
	}
	if !e.actorMatches(sv, "Caster", 1) {
		t.Fatal("the enchanted creature's controller must be the scoped caster")
	}
	if e.actorMatches(sv, "Caster", 0) {
		t.Fatal("the aura's own controller must not be scoped as the enchanted creature's controller")
	}
}
