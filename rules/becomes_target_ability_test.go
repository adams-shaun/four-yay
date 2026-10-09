package rules

// Mode$ BecomesTarget with a ValidSource$ Ability* spelling -- Loki, God of
// Mischief's "Whenever a player or permanent becomes the target of an ability
// you control, draw a card. This ability triggers only once each turn."
// (`.cards/cardsfolder/l/loki_god_of_mischief.txt`).
//
// The engine's becomesTargetMatches reads ValidSource$ through
// becomesTargetSourceMatches, whose `Ability` base is an ability-shape test
// the ordinary object-filter grammar has no base word for (an ability stack
// object carries no card types), and its ValidTarget$ "Player" half is a
// player target the event carries as Amount 1/3, not in ev.IDs. These tests
// drive the REAL corpus card end to end.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// lokiFixture asserts the corpus drift preconditions every test below depends
// on: the trigger line is exactly the Ability-sourced player-or-permanent
// BecomesTarget with the once-per-turn limit.
func lokiFixture(t *testing.T, reg *cards.Registry) *cards.Trigger {
	t.Helper()
	lokiCard := mustCorpusCard(t, reg, "Loki, God of Mischief")
	trig := corpusTriggerMode(t, lokiCard, "BecomesTarget")
	if trig.Params["ValidSource"] != "Ability.YouCtrl" {
		t.Fatalf("corpus fixture drift: Loki's ValidSource$ = %q, want Ability.YouCtrl", trig.Params["ValidSource"])
	}
	if trig.Params["ValidTarget"] != "Player,Permanent" {
		t.Fatalf("corpus fixture drift: Loki's ValidTarget$ = %q, want Player,Permanent", trig.Params["ValidTarget"])
	}
	if trig.Params["ActivationLimit"] != "1" {
		t.Fatalf("corpus fixture drift: Loki's ActivationLimit$ = %q, want 1", trig.Params["ActivationLimit"])
	}
	return trig
}

// targetPlayerOptionIdx returns the pending KTarget decision's option index
// naming the player, failing when the ask does not offer it.
func targetPlayerOptionIdx(t *testing.T, e *Engine, p state.PlayerID) int {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected a target decision, got %+v", d)
	}
	for _, o := range d.Options {
		if o.Kind == "player" && o.Player == p {
			return o.Index
		}
	}
	t.Fatalf("player %d not offered: %+v", p, d.Options)
	return -1
}

// activateProdigalAtPlayer activates seat 0's Prodigal Sorcerer's targeted {T}
// ping at seat p and drains the stack.
func activateProdigalAtPlayer(t *testing.T, e *Engine, sorcerer state.ObjID, target state.PlayerID) {
	t.Helper()
	if o := e.G.Obj(sorcerer); o == nil || o.Tapped || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Prodigal Sorcerer is not an untapped battlefield permanent: %+v", o)
	}
	e.askPriority(0)
	opt := abilityOption(t, e, sorcerer, 0)
	submitChoices(t, e, opt.Index)
	submitChoices(t, e, targetPlayerOptionIdx(t, e, target))
	passUntilStackEmpty(t, e, 40)
}

// TestLokiBecomesTargetAbilityFiresOnOwnPing drives the real card: p0's
// Prodigal Sorcerer ping at p1 (a player target from an ability p0 controls)
// draws p0 a card, and the once-per-turn limit stops the second ping from
// drawing again while the ping itself still resolves.
func TestLokiBecomesTargetAbilityFiresOnOwnPing(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	lokiFixture(t, reg)

	e, ids := becomesTargetOnceBoard(t, reg,
		[]string{"Loki, God of Mischief", "Prodigal Sorcerer"},
		nil)
	loki := ids["Loki, God of Mischief"]
	sorcerer := ids["Prodigal Sorcerer"]
	if o := e.G.Obj(loki); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Loki is not on the battlefield: %+v", o)
	}
	if e.G.Players[1].Life != 20 {
		t.Fatalf("precondition: p1 life = %d, want 20", e.G.Players[1].Life)
	}

	activateProdigalAtPlayer(t, e, sorcerer, 1)
	if got := e.G.Players[1].Life; got != 19 {
		t.Fatalf("the ping did not resolve: p1 life = %d, want 19", got)
	}
	if got := len(e.G.Zone(state.ZHand, 0)); got != 1 {
		t.Fatalf("Loki's Ability.YouCtrl BecomesTarget trigger did not fire on the ability's player target: hand = %d, want 1", got)
	}
	if o := e.G.Obj(loki); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Loki left the battlefield before the limit probe: %+v", o)
	}

	// Setup-only untap: the first ping consumed the Sorcerer's {T}, and the
	// ActivationLimit$ 1 probe needs a second legal activation the same turn.
	e.G.Obj(sorcerer).Tapped = false
	activateProdigalAtPlayer(t, e, sorcerer, 1)
	if got := e.G.Players[1].Life; got != 18 {
		t.Fatalf("the second ping did not resolve: p1 life = %d, want 18", got)
	}
	if got := len(e.G.Zone(state.ZHand, 0)); got != 1 {
		t.Fatalf("the second ping drew to %d, want 1: ActivationLimit$ 1 must stop the second trigger", got)
	}
}

// TestLokiBecomesTargetAbilityIgnoresASpell proves the Ability base is a
// spell/ability split: p0's own Shock targeting p1 matches ValidTarget$'s
// Player half, but a spell is not an ability, so the trigger must not fire.
func TestLokiBecomesTargetAbilityIgnoresASpell(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	lokiFixture(t, reg)

	e, ids := becomesTargetOnceBoard(t, reg,
		[]string{"Loki, God of Mischief"},
		nil)
	loki := ids["Loki, God of Mischief"]
	if o := e.G.Obj(loki); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Loki is not on the battlefield: %+v", o)
	}
	if e.G.Players[1].Life != 20 {
		t.Fatalf("precondition: p1 life = %d, want 20", e.G.Players[1].Life)
	}

	shock := e.G.AddObject(mustCorpusCard(t, reg, "Shock"), 0)
	shock.Zone = state.ZHand
	e.G.SetZone(state.ZHand, 0, append(e.G.Zone(state.ZHand, 0), shock.ID))
	e.G.Players[0].Pool[state.MR] = 1

	e.G.Active, e.G.Priority = 0, 0
	e.askPriority(0)
	submitChoices(t, e, passToCast(t, e, shock.ID))
	submitChoices(t, e, targetPlayerOptionIdx(t, e, 1))
	passUntilStackEmpty(t, e, 40)

	// The spell really resolved (the feature's handler ran) and yet drew
	// nothing: a spell's stack object does not answer the Ability base.
	if got := e.G.Players[1].Life; got != 18 {
		t.Fatalf("Shock did not resolve onto p1: p1 life = %d, want 18", got)
	}
	if got := len(e.G.Zone(state.ZHand, 0)); got != 0 {
		t.Fatalf("a spell's player target fired Loki's Ability-only trigger: hand = %d, want 0", got)
	}
}
