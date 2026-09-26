package rules

// combatres-cantattack: the conditional parameter family on a face CantAttack
// static. Before this task, effects.CantRestrictionParamsReadable rejected a
// static carrying UnlessDefender$, Condition$ or CheckSVar$, so
// rules/layers.go's attackBlocked skipped it whole. attackBlocked now reads
// the family: continuousGateHolds evaluates IsPresent$/CheckSVar$/SVarCompare$/
// Condition$/ClassBand$ and effects.UnlessDefenderHolds evaluates
// UnlessDefender$ against the defending player, so a line carrying them is
// ENFORCED instead of silently dropped.
//
// Every UnlessDefender$/CheckSVar$ leaf below drives a REAL corpus card
// through e.attackBlocked (the pure per-(attacker, defender) read the offer
// filter and the validator share). The Condition$ leaf uses a synthetic face
// because no corpus CantAttack carrier spells an evaluable Condition$ (the
// two carriers, ExtraTurn and Monarch, are values continuousConditionHolds
// deliberately fails closed on) -- it pins the gate extension itself.
//
// Each test asserts its own precondition (the carrier is on the battlefield,
// the discriminating value is actually absent/present) so a vacuous fixture
// fails loudly rather than passing.

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// attackBlockedRegressionEngine is the shared two-seat fixture: seat 0 is the
// protagonist and the active player, and the seed is advanced until the CR
// 103.1 toss starts seat 0 (the layerEngine convention), so the Condition$
// PlayerTurn leaf can flip the active seat with a real TurnChange.
func attackBlockedRegressionEngine(t *testing.T) *Engine {
	t.Helper()
	return layerEngine(t)
}

// TestCantAttackUnlessDefenderControlsIsland drives Kukemssa Serpent's real
// corpus static
// `S:Mode$ CantAttack | ValidCard$ Card.Self | UnlessDefender$ controlsIsland`.
// The creature may attack only when the DEFENDING player controls an Island:
// with none it is blocked, and an Island on the defender's battlefield
// releases it -- both directions measured through attackBlocked.
func TestCantAttackUnlessDefenderControlsIsland(t *testing.T) {
	e := attackBlockedRegressionEngine(t)
	serpent := onBoardCard(t, e, 0, corpusCard(t, "Kukemssa Serpent"))
	if e.G.Obj(serpent).Zone != state.ZBattlefield {
		t.Fatal("precondition: Kukemssa Serpent is not on the battlefield")
	}
	if !e.attackBlocked(serpent, 1, 0) {
		t.Fatal("Kukemssa Serpent attacked although the defending player controlled no Island (UnlessDefender$ controlsIsland unread)")
	}
	island := onBoard(t, e, 1, "Name:Island\nTypes:Basic Land Island\nPT:0/0\nOracle:x\n")
	if o := e.G.Obj(island); o == nil || o.Zone != state.ZBattlefield || o.Controller != 1 {
		t.Fatalf("precondition: the defender's Island is not on seat 1's battlefield: %+v", e.G.Obj(island))
	}
	if e.attackBlocked(serpent, 1, 0) {
		t.Fatal("Kukemssa Serpent remained blocked although the defending player controlled an Island")
	}
}

// TestCantAttackUnlessDefenderHasFewerCreatures drives Mogg Toady's real
// corpus static
// `S:Mode$ CantAttack | ValidCard$ Card.Self | UnlessDefender$ hasFewerCreaturesInPlayThanYou`.
// The creature may attack only while the DEFENDER controls strictly fewer
// creatures than the Toady's controller. With the defender behind it is
// released; at parity the predicate fails and it is blocked.
func TestCantAttackUnlessDefenderHasFewerCreatures(t *testing.T) {
	e := attackBlockedRegressionEngine(t)
	toady := onBoardCard(t, e, 0, corpusCard(t, "Mogg Toady"))
	onBoard(t, e, 0, "Name:Ally A\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	onBoard(t, e, 0, "Name:Ally B\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	onBoard(t, e, 1, "Name:Foe A\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	if e.G.Obj(toady).Zone != state.ZBattlefield {
		t.Fatal("precondition: Mogg Toady is not on the battlefield")
	}
	if n := len(e.G.Zone(state.ZBattlefield, 0)); n != 3 {
		t.Fatalf("precondition: seat 0 controls %d creatures, want 3", n)
	}
	if n := len(e.G.Zone(state.ZBattlefield, 1)); n != 1 {
		t.Fatalf("precondition: seat 1 controls %d creatures, want 1", n)
	}
	if e.attackBlocked(toady, 1, 0) {
		t.Fatal("Mogg Toady was blocked although the defender controlled fewer creatures (UnlessDefender$ hasFewerCreaturesInPlayThanYou unread)")
	}
	// Parity: the defender no longer has fewer, so the restriction bites.
	onBoard(t, e, 1, "Name:Foe B\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	onBoard(t, e, 1, "Name:Foe C\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	if n := len(e.G.Zone(state.ZBattlefield, 1)); n != 3 {
		t.Fatalf("precondition: seat 1 controls %d creatures, want 3", n)
	}
	if !e.attackBlocked(toady, 1, 0) {
		t.Fatal("Mogg Toady attacked at creature parity, where the defender does not have fewer")
	}
}

// TestCantAttackUnlessDefenderNegated drives Veteran Brawlers's real corpus
// static
// `S:Mode$ CantAttack | ValidCard$ Card.Self | UnlessDefender$ !controlsLand.untapped`
// ("can't attack if defending player controls an untapped land"). The leading
// `!` inverts the predicate: an untapped land on the defender's battlefield
// BLOCKS, and a tapped-only land releases. This is the negation half of the
// UnlessDefender$ evaluator.
func TestCantAttackUnlessDefenderNegated(t *testing.T) {
	e := attackBlockedRegressionEngine(t)
	brawlers := onBoardCard(t, e, 0, corpusCard(t, "Veteran Brawlers"))
	if e.G.Obj(brawlers).Zone != state.ZBattlefield {
		t.Fatal("precondition: Veteran Brawlers is not on the battlefield")
	}
	land := onBoard(t, e, 1, "Name:Defender Land\nTypes:Land\nPT:0/0\nOracle:x\n")
	if o := e.G.Obj(land); o == nil || o.Tapped {
		t.Fatal("precondition: the defender's land must start untapped")
	}
	if !e.attackBlocked(brawlers, 1, 0) {
		t.Fatal("Veteran Brawlers attacked although the defender controlled an untapped land (!controlsLand.untapped unread)")
	}
	e.G.Obj(land).Tapped = true
	if e.attackBlocked(brawlers, 1, 0) {
		t.Fatal("Veteran Brawlers stayed blocked although the defender's only land is tapped")
	}
}

// TestCantAttackUnlessDefenderHasCardsInGraveyard drives Vantress Gargoyle's
// real corpus static
// `S:Mode$ CantAttack | ValidCard$ Card.Self | UnlessDefender$ HasCardsInGraveyard_Card_GE7`.
// Seven cards in the DEFENDER's graveyard release it; fewer block it. This is
// the HasCardsIn<zone>_<type>_<cmp> half of the UnlessDefender$ evaluator.
func TestCantAttackUnlessDefenderHasCardsInGraveyard(t *testing.T) {
	e := attackBlockedRegressionEngine(t)
	gargoyle := onBoardCard(t, e, 0, corpusCard(t, "Vantress Gargoyle"))
	if e.G.Obj(gargoyle).Zone != state.ZBattlefield {
		t.Fatal("precondition: Vantress Gargoyle is not on the battlefield")
	}
	for i := 0; i < 6; i++ {
		addToGraveyardType(t, e, 1, "Sorcery")
	}
	if n := len(e.G.Zone(state.ZGraveyard, 1)); n != 6 {
		t.Fatalf("precondition: defender graveyard holds %d cards, want 6", n)
	}
	if !e.attackBlocked(gargoyle, 1, 0) {
		t.Fatal("Vantress Gargoyle attacked below seven defender graveyard cards (HasCardsInGraveyard_Card_GE7 unread)")
	}
	addToGraveyardType(t, e, 1, "Sorcery")
	if n := len(e.G.Zone(state.ZGraveyard, 1)); n != 7 {
		t.Fatalf("precondition: defender graveyard holds %d cards, want 7", n)
	}
	if e.attackBlocked(gargoyle, 1, 0) {
		t.Fatal("Vantress Gargoyle stayed blocked at seven defender graveyard cards")
	}
}

// TestCantAttackCheckSVarGate drives Deep-Sea Terror's real corpus static
// `S:Mode$ CantAttack | ValidCard$ Card.Self | CheckSVar$ X | SVarCompare$ LT7`
// with `SVar:X:Count$ValidGraveyard Card.YouOwn`. The creature can't attack
// unless seven or more cards are in its controller's graveyard: at six the
// gate holds and it is blocked, at seven the gate fails and it may attack.
// This exercises the CheckSVar$/SVarCompare$ half through continuousGateHolds.
func TestCantAttackCheckSVarGate(t *testing.T) {
	e := attackBlockedRegressionEngine(t)
	terror := onBoardCard(t, e, 0, corpusCard(t, "Deep-Sea Terror"))
	if o := e.G.Obj(terror); o == nil || o.Zone != state.ZBattlefield {
		t.Fatal("precondition: Deep-Sea Terror is not on the battlefield")
	}
	for i := 0; i < 6; i++ {
		addToGraveyardType(t, e, 0, "Sorcery")
	}
	if n := len(e.G.Zone(state.ZGraveyard, 0)); n != 6 {
		t.Fatalf("precondition: controller graveyard holds %d cards, want 6", n)
	}
	if !e.attackBlocked(terror, 1, 0) {
		t.Fatal("Deep-Sea Terror attacked with fewer than seven graveyard cards (CheckSVar$ X LT7 unread)")
	}
	addToGraveyardType(t, e, 0, "Sorcery")
	if n := len(e.G.Zone(state.ZGraveyard, 0)); n != 7 {
		t.Fatalf("precondition: controller graveyard holds %d cards, want 7", n)
	}
	if e.attackBlocked(terror, 1, 0) {
		t.Fatal("Deep-Sea Terror stayed blocked at seven graveyard cards, where its CheckSVar$ gate fails")
	}
}

// TestCantAttackConditionGate pins the Condition$ half of the family with a
// synthetic face (no corpus CantAttack carrier spells an evaluable
// Condition$): `Condition$ PlayerTurn` holds only while the static's
// controller is the active player. attackBlocked is a pure read, so the same
// board is probed at seat 0's turn (gate holds, blocked) and after a real
// TurnChange to seat 1 (gate fails, released).
func TestCantAttackConditionGate(t *testing.T) {
	e := attackBlockedRegressionEngine(t)
	bear := onBoard(t, e, 0, "Name:Conditional Attacker\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\n"+
		"S:Mode$ CantAttack | ValidCard$ Card.Self | Condition$ PlayerTurn | Description$ CARDNAME can't attack unless it is your turn.\nOracle:x\n")
	if o := e.G.Obj(bear); o == nil || o.Zone != state.ZBattlefield {
		t.Fatal("precondition: the conditional attacker is not on the battlefield")
	}
	if e.G.Active != 0 {
		t.Fatalf("precondition: seat 0 must be the active player, got %d", e.G.Active)
	}
	if !e.attackBlocked(bear, 1, 0) {
		t.Fatal("the Condition$ PlayerTurn gate did not hold on the controller's own turn (Condition$ unread)")
	}
	e.emit(events.Event{Kind: events.TurnChange, Player: 1, Amount: 2})
	if e.G.Active != 1 {
		t.Fatalf("precondition: the TurnChange did not make seat 1 active, got %d", e.G.Active)
	}
	if e.attackBlocked(bear, 1, 0) {
		t.Fatal("the CantAttack static stayed enforced although its Condition$ PlayerTurn gate no longer holds")
	}
}
