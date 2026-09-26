package rules

// comma-static-1: compound `S:Mode$ A,B` statics reach the runtime as ONE
// static per mode (the parse-time split in cards/parse.go, cards/comma_static_test.go
// pins the parser surface). This file pins the gameplay half on a real corpus
// carrier: Pacifism's
//
//	S:Mode$ CantAttack,CantBlock | ValidCard$ Creature.EnchantedBy
//
// must suppress BOTH halves on the enchanted creature — the bear is offered
// neither as an attacker (KAttackers option filter through attackBlocked,
// rules/layers.go) nor as a blocker (askBlockers' option filter through
// blockRestricted, rules/statics.go). Before the split the whole line was one
// opaque static name neither consumer recognised, so the bear attacked and
// blocked freely.
//
// Every fixture mutation goes through e.emit, so the test ends replay-verified.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func TestPacifismCommaModeStaticSuppressesAttackAndBlock(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	pac, ok := reg.Lookup("Pacifism")
	if !ok {
		t.Fatal("corpus missing Pacifism")
	}
	// Precondition: the corpus card still prints the compound shape this
	// ticket splits (both halves present after the parse-time split).
	var hasAttack, hasBlock bool
	for _, f := range pac.Faces {
		for _, st := range f.Statics {
			switch st.Mode {
			case "CantAttack":
				hasAttack = true
			case "CantBlock":
				hasBlock = true
			}
		}
	}
	if !hasAttack || !hasBlock {
		t.Fatalf("corpus Pacifism lacks the split CantAttack/CantBlock statics (hasAttack=%v hasBlock=%v): %+v",
			hasAttack, hasBlock, pac.Faces[0].Statics)
	}
	bear := card(t, staticBearFixture)
	pacified := card(t, staticBearFixture) // the bear Pacifism enchants
	e, cfg := restrictionGame(t, 6113,
		[][]*cards.Card{{pac}, nil, nil},
		[][]*cards.Card{nil, {bear, pacified}, {bear}})
	bear1 := bearOnBoard(t, e, 1, bear)
	victim := bearOnBoard(t, e, 1, pacified)
	bear2 := bearOnBoard(t, e, 2, bear)

	// Enchant seat 1's second bear with Pacifism ({1}{W}).
	pacID := findAndMoveToHand(t, e, 0, "Pacifism")
	addMana(t, e, 0, "CCW")
	castFromPriority(t, e, pacID)
	answerKTarget(t, e, victim)
	passUntilStackEmpty(t, e, 60)
	if e.G.Obj(pacID).AttachedTo != victim {
		t.Fatalf("pacifism AttachedTo = %d, want the enchanted bear %d", e.G.Obj(pacID).AttachedTo, victim)
	}

	// Direct reads: BOTH halves suppress on the enchanted bear, and the
	// control bear (precondition that the two really differ) suppresses
	// neither.
	if !e.attackBlocked(victim, 0, 0) || !e.attackBlocked(victim, 2, 0) {
		t.Fatalf("pacified bear attack pairs not blocked: (→0)=%v (→2)=%v",
			e.attackBlocked(victim, 0, 0), e.attackBlocked(victim, 2, 0))
	}
	if !e.blockRestricted(victim, bear2) {
		t.Fatal("pacified bear not restricted from blocking: the CantBlock half never reached blockRestricted")
	}
	if e.attackBlocked(bear1, 0, 0) || e.blockRestricted(bear1, bear2) {
		t.Fatal("control bear (precondition) is restricted — the fixture does not isolate the enchanted one")
	}

	// The real offer path, attacks: at seat 1's declare-attackers the
	// KAttackers decision must offer the control bear a pair and never the
	// pacified one.
	driveToStep(t, e, 3, 1, state.StepDeclareAttackers)
	d := e.Pending()
	if d == nil || d.Kind != decision.KAttackers {
		t.Fatalf("expected the attackers decision at seat 1's declare step, got %+v", d)
	}
	if opts := attackerOptionsFor(e, victim); len(opts) != 0 {
		t.Fatalf("pacified bear still offered as an attacker: %+v", opts)
	}
	if opts := attackerOptionsFor(e, bear1); len(opts) == 0 {
		t.Fatal("precondition failed: the control bear has no attack pair to offer")
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{}}); err != nil {
		t.Fatalf("declare no attackers: %v", err)
	}

	// The real offer path, blocks: seat 2 attacks seat 1; seat 1's blockers
	// decision must offer the control bear and never the pacified one.
	driveToStep(t, e, 4, 2, state.StepDeclareAttackers)
	d2 := e.Pending()
	if d2 == nil || d2.Kind != decision.KAttackers {
		t.Fatalf("expected the attackers decision at seat 2's declare step, got %+v", d2)
	}
	var atk []int
	for _, o := range d2.Options {
		if o.Obj == bear2 && o.Player == 1 {
			atk = append(atk, o.Index)
		}
	}
	if len(atk) != 1 {
		t.Fatalf("seat 2's bear offered %+v, want exactly one pair against seat 1", d2.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d2.Seq, Player: d2.Player, Choices: atk}); err != nil {
		t.Fatalf("declare seat-2 attack: %v", err)
	}
	drainCombatPriority(t, e)
	d3 := e.Pending()
	if d3 == nil || d3.Kind != decision.KBlockers {
		t.Fatalf("expected the blockers decision at seat 1, got %+v", d3)
	}
	for _, o := range d3.Options {
		if o.Obj == victim {
			t.Fatalf("pacified bear still offered as a blocker: %+v", d3.Options)
		}
	}
	var blockOffer int
	for _, o := range d3.Options {
		if o.Obj == bear1 {
			blockOffer++
		}
	}
	if blockOffer == 0 {
		t.Fatal("precondition failed: the control bear has no block pair to offer")
	}
	if err := e.Submit(decision.Intent{Seq: d3.Seq, Player: d3.Player, Choices: []int{}}); err != nil {
		t.Fatalf("declare no blockers: %v", err)
	}
	replayCheck(t, e, cfg)
}
