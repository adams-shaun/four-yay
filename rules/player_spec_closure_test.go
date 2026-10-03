package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules/trigmatch"
	"github.com/adams-shaun/gorge/state"
)

// The real Electryte filter names a combat defending player. A prevention
// Note on a noncombat hit also has a recipient, but has no defending role.
func TestPlayerSpecPreventedNoncombatHasNoDefender(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, _, ids := dsBoard(t, reg, "Electryte")
	source := ids["Electryte"]
	if e.G.Obj(source).Zone != state.ZBattlefield {
		t.Fatal("precondition: Electryte not on battlefield")
	}
	trig := fx20TriggerWithParam(t, e, source, "Player.TriggeredDefendingPlayer")
	if trig.Params["ValidTarget"] != "Player.TriggeredDefendingPlayer" {
		t.Fatalf("precondition: wrong filter: %q", trig.Params["ValidTarget"])
	}
	ev := events.Event{Kind: events.Note, Player: 1, Amount: 2, Text: "prevented: protection"}
	if ev.Player == e.controllerOf(source) {
		t.Fatal("precondition: recipient equals source controller")
	}
	e.combatDamaging = false
	if trigmatch.DamagePreventedMatches(boardOf(e), trig, source, ev) {
		t.Fatal("noncombat prevention has no defending-player role")
	}
	e.combatDamaging = true
	if !trigmatch.DamagePreventedMatches(boardOf(e), trig, source, ev) {
		t.Fatal("combat prevention must bind the defending player")
	}
}

// Broodrage Mycoid's You.descended trigger reads a permanent CARD entering
// its owner's graveyard, even when controlled by another player.
func TestPlayerSpecBroodrageDescendedFromAnyZone(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, _, ids := dsBoardWith(t, reg, mustCorpusCard(t, reg, "Grizzly Bears"), "Broodrage Mycoid", "Grizzly Bears", "Lightning Bolt")
	mycoid := ids["Broodrage Mycoid"]
	bear := ids["Grizzly Bears"]
	bolt := ids["Lightning Bolt"]
	trig := fx20TriggerWithParam(t, e, mycoid, "You.descended")
	if trig.Params["ValidPlayer"] != "You.descended" || e.G.Obj(mycoid).Zone != state.ZBattlefield || e.G.Obj(bear).Zone != state.ZBattlefield {
		t.Fatal("precondition: trigger and permanents not on battlefield")
	}
	e.G.Active = 0
	step := events.Event{Kind: events.StepChange}
	if trigmatch.PhaseMatches(boardOf(e), trig, mycoid, step) {
		t.Fatal("no permanent card has entered our graveyard this turn")
	}
	// An instant card is not a permanent card even when it moves from the
	// battlefield in a synthetic fixture; its entry must not cause descent.
	if e.G.Obj(bolt).Face().IsPermanent() || e.G.Obj(bolt).Zone != state.ZBattlefield {
		t.Fatal("precondition: Bolt must be a nonpermanent card in the fixture")
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: bolt, From: state.ZBattlefield, To: state.ZGraveyard})
	if trigmatch.PhaseMatches(boardOf(e), trig, mycoid, step) {
		t.Fatal("a nonpermanent card does not cause descent")
	}
	replayState := e.G.Clone()
	moveEvent := e.emit(events.Event{Kind: events.MoveZone, Obj: bear, From: state.ZBattlefield, To: state.ZGraveyard})
	events.Apply(replayState, moveEvent)
	if !effects.MatchesPlayerSpec(replayState, "You.descended", 0, 0) {
		t.Fatal("descend provenance must reconstruct by folding the logged move")
	}
	if e.G.Obj(bear).Zone != state.ZGraveyard || e.G.Obj(bear).Owner != 0 {
		t.Fatal("precondition: our permanent card did not enter our graveyard")
	}
	if !trigmatch.PhaseMatches(boardOf(e), trig, mycoid, step) {
		t.Fatal("Broodrage Mycoid must trigger after our permanent card descended")
	}
	if effects.MatchesPlayerSpec(e.G, "Player.descended", 1, 0) {
		t.Fatal("other seat did not descend")
	}
	e.emit(events.Event{Kind: events.TurnChange, Player: 1})
	if effects.MatchesPlayerSpec(e.G, "Player.descended", 0, 0) {
		t.Fatal("descend history must expire at turn change")
	}
}

// Curse of the Pierced Heart is a real Enchant:Player Aura: casting it must
// create a replayable player attachment rather than an unattached Aura SBA.
func TestPlayerSpecCurseEnchantPlayer(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	curse := mustCorpusCard(t, reg, "Curse of the Pierced Heart")
	e := handEngine(t, curse)
	id := e.G.Zone(state.ZHand, 0)[0]
	if e.G.Obj(id).Face() == nil || e.G.Obj(id).Face().Name != "Curse of the Pierced Heart" {
		t.Fatal("precondition: wrong card in hand")
	}
	addMana(t, e, 0, "RR")
	found := false
	for _, opt := range castOptions(t, e) {
		if opt.Obj == id {
			submitChoices(t, e, opt.Index)
			found = true
			break
		}
	}
	if !found {
		t.Fatal("Curse cast not offered")
	}
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected player target decision: %+v", d)
	}
	index := -1
	for _, o := range d.Options {
		if o.Player == 1 && o.Kind == "player" {
			index = o.Index
		}
	}
	if index < 0 {
		t.Fatalf("opponent target missing: %+v", d.Options)
	}
	submitChoices(t, e, index)
	passUntilStackEmpty(t, e, 20)
	o := e.G.Obj(id)
	if o.Zone != state.ZBattlefield || !o.HasAttachedPlayer || o.AttachedPlayer != 1 {
		t.Fatalf("curse not attached to seat 1: %+v", o)
	}
	if !effects.MatchesPlayerSpec(e.G, "Player.EnchantedBy", 1, 0) || effects.MatchesPlayerSpec(e.G, "Player.EnchantedBy", 0, 0) {
		t.Fatal("EnchantedBy must name only the enchanted player")
	}
	replayState := e.G.Clone()
	attachEvent := e.emit(events.Event{Kind: events.Attach, Obj: id, Player: 0, Text: "attach to player"})
	events.Apply(replayState, attachEvent)
	if replayState.Obj(id).AttachedPlayer != e.G.Obj(id).AttachedPlayer ||
		replayState.Obj(id).HasAttachedPlayer != e.G.Obj(id).HasAttachedPlayer {
		t.Fatal("player attachment must reconstruct by folding the logged event")
	}
	if !effects.MatchesPlayerSpec(e.G, "Player.EnchantedBy", 0, 0) || effects.MatchesPlayerSpec(e.G, "Player.EnchantedBy", 1, 0) {
		t.Fatal("reattaching to seat zero must not be mistaken for detaching")
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZBattlefield, To: state.ZGraveyard})
	if effects.MatchesPlayerSpec(e.G, "Player.EnchantedBy", 0, 0) {
		t.Fatal("a departed Aura must not continue enchanting the player")
	}
}
