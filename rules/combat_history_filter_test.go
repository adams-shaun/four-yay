package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func TestVengefulAncestorGoadedAttackerFilter(t *testing.T) {
	t.Parallel()
	e, ids := pcdrEngine(t, "Vengeful Ancestor", "Grizzly Bears")
	ancestor, bear := ids["Vengeful Ancestor"], ids["Grizzly Bears"]
	if e.G.Obj(ancestor).Zone != state.ZBattlefield || e.G.Obj(bear).Zone != state.ZBattlefield {
		t.Fatal("precondition: the Ancestor and attacker must be on the battlefield")
	}

	tr := cards.Trigger{}
	for _, candidate := range e.G.Obj(ancestor).Face().Triggers {
		if candidate.Mode == "Attacks" && candidate.Params["ValidCard"] == "Creature.IsGoaded" {
			tr = candidate
			break
		}
	}
	if tr.Mode == "" {
		t.Fatal("corpus fixture no longer has Vengeful Ancestor's goaded-attacker trigger")
	}
	attack := events.Event{Kind: events.DeclareAttackers, Player: 1, IDs: []state.ObjID{bear}}
	if e.triggerMatches(tr, ancestor, attack, nil) {
		t.Fatal("ungoaded creature unexpectedly matched the trigger")
	}
	e.emit(events.Event{Kind: events.Goad, Obj: bear, Player: 0})
	if !effects.MatchesObjectCtx(e.G, "Creature.IsGoaded", e.G.Obj(bear), effects.SpecContext{}) {
		t.Fatal("the event-backed goad designation is not visible to the filter")
	}
	if !e.triggerMatches(tr, ancestor, attack, nil) {
		t.Fatal("Vengeful Ancestor's trigger did not match the goaded attacker")
	}
	life := e.G.Players[0].Life
	e.pendingTriggers = nil // discard the ETB goad trigger from fixture setup
	e.emit(attack)
	if len(e.pendingTriggers) != 1 {
		t.Fatalf("expected the goaded-attacker damage trigger to queue, got %d triggers", len(e.pendingTriggers))
	}
	e.putTriggersOnStack()
	if len(e.G.Stack) != 1 {
		t.Fatalf("expected one triggered ability on the stack, got %v", e.G.Stack)
	}
	e.resolveTop()
	if got := e.G.Players[0].Life; got != life-1 {
		t.Fatalf("goaded attacker's controller life = %d, want %d after the 1 damage trigger", got, life-1)
	}
}

func TestSteelHellkiteCombatDamageControllerFilter(t *testing.T) {
	t.Parallel()
	e, ids := pcdrEngine(t, "Steel Hellkite", "Grizzly Bears", "Runeclaw Bear")
	hellkite := ids["Steel Hellkite"]
	victim := ids["Grizzly Bears"]
	unhit := ids["Runeclaw Bear"]
	e.emit(events.Event{Kind: events.ControlChange, Obj: victim, Player: 1})
	for _, id := range []state.ObjID{hellkite, victim, unhit} {
		if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
			t.Fatalf("precondition: object %d must be on the battlefield", id)
		}
	}
	if e.G.Obj(victim).Controller != 1 || e.G.Obj(unhit).Controller != 0 {
		t.Fatal("precondition: the two same-mana-value permanents need different controllers")
	}
	for _, id := range []state.ObjID{victim, unhit} {
		if !effects.MatchesObjectCtx(e.G, "Permanent.cmcEQ2", e.G.Obj(id), effects.SpecContext{}) {
			t.Fatalf("precondition: object %d must have mana value 2", id)
		}
	}

	e.pending = nil
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 2})
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepMain1})
	pcdrCombat(e, hellkite, 1, 5)
	hits := e.CombatDamageToPlayersThisTurn()
	if len(hits) != 1 || hits[0].Source != hellkite || hits[0].Player != 1 {
		t.Fatalf("precondition: expected Hellkite combat damage recorded against seat 1, got %+v", hits)
	}
	filterContext := effects.SpecContext{Source: hellkite, CombatDamageHits: hits, ManaValue: 2, HasManaValue: true}
	if !effects.MatchesObjectCtx(e.G, "Permanent.controllerWasDealtCombatDamageByThisTurn", e.G.Obj(victim), filterContext) {
		t.Fatal("precondition: bound history predicate must match the hit player's permanent")
	}
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "C", Amount: 2})
	e.priorityRound()
	opt := abilityOption(t, e, hellkite, 1)
	submitChoices(t, e, opt.Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || len(d.Options) == 0 || d.Options[0].Kind != "x" {
		t.Fatalf("Hellkite X announcement = %+v, want the X choice", d)
	}
	chosenTwo := -1
	for _, option := range d.Options {
		if option.Label == "X = 2" {
			chosenTwo = option.Index
		}
	}
	if chosenTwo < 0 {
		t.Fatalf("Hellkite did not offer X=2: %+v", d.Options)
	}
	submitChoices(t, e, chosenTwo)
	if len(e.G.Stack) != 1 {
		t.Fatalf("precondition: expected one activated ability on the stack, got %v", e.G.Stack)
	}
	wrapper := e.G.Obj(e.G.Stack[0])
	if wrapper == nil || wrapper.Ability == nil || wrapper.Source != hellkite || wrapper.Ability.API != "DestroyAll" {
		t.Fatalf("precondition: expected Hellkite's DestroyAll activation, got %+v", wrapper)
	}
	passUntilStackEmpty(t, e, 30)
	if e.G.Obj(victim).Zone != state.ZGraveyard {
		t.Fatalf("Steel Hellkite's filter did not destroy the matching permanent; victim zone=%s", e.G.Obj(victim).Zone)
	}
	if e.G.Obj(unhit).Zone != state.ZBattlefield {
		t.Fatal("Steel Hellkite's filter destroyed a same-mana-value permanent controlled by an undamaged player")
	}
}
