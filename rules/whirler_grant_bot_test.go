package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// whirlerRogueBoard builds the fb-20260927T153930Z board with REAL card flow:
// Whirler Rogue plus two untapped artifacts for seat 0, an enemy creature for
// seat 1, and -- when ownCreature -- an own creature. The engine is driven to
// a posed main1 priority decision for seat 0, so the option list the tests
// read is the one legalActions actually offers.
func whirlerRogueBoard(t *testing.T, ownCreature bool) (*Engine, state.ObjID) {
	t.Helper()
	e := handEngine(t)
	whirler := onBoardCard(t, e, 0, corpusCard(t, "Whirler Rogue"))
	art := card(t, "Name:Dud Mana Rock\nTypes:Artifact\nOracle:x\n")
	onBoardCard(t, e, 0, art)
	onBoardCard(t, e, 0, card(t, "Name:Dud Mana Rock\nTypes:Artifact\nOracle:x\n"))
	onBoardCard(t, e, 1, card(t, "Name:Enemy Bear\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"))
	if ownCreature {
		onBoardCard(t, e, 0, card(t, "Name:Own Giant\nTypes:Creature Giant\nPT:6/6\nOracle:x\n"))
	}
	e.priorityRound()
	return e, whirler
}

// findWhirlerAbilityOption returns the offered "ability" option for Whirler
// Rogue's activation, or fails the test when it is absent. Precondition
// helper: a decline assertion must prove the activation was offered, so the
// policy's pass is a choice and not the engine withholding the option.
func findWhirlerAbilityOption(t *testing.T, e *Engine, whirler state.ObjID) decision.Option {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("fixture precondition: pending decision = %+v, want KPriority", d)
	}
	for _, o := range d.Options {
		if o.Kind == "ability" && o.Obj == whirler {
			return o
		}
	}
	t.Fatal("fixture precondition: Whirler Rogue's activation was not offered")
	return decision.Option{}
}

// passIdx returns the offered "pass" option index, failing when absent.
func passIdx(t *testing.T, e *Engine) int {
	t.Helper()
	d := e.Pending()
	if d == nil {
		t.Fatal("fixture precondition: no pending decision")
	}
	for _, o := range d.Options {
		if o.Kind == "pass" {
			return o.Index
		}
	}
	t.Fatal("fixture precondition: no pass option offered")
	return -1
}

// TestBotDeclinesWhirlerGrantWithoutOwnCreature is the fb-20260927T153930Z
// regression, the ACTIVATION half: Whirler Rogue and two untapped artifacts
// are on the bot's side but its only creature is Whirler Rogue itself (the
// enemy has the other one). Activating "Target creature can't be blocked
// this turn" could then only sensibly aim the grant at an opponent's
// creature, so the bot must pass instead. The decline comes from the ability
// scorer (A1c) reading the offer-time GrantStatics polarity over a census
// that EXCLUDES the source permanent -- with the source included,
// Whirler Rogue would always count as its own sensible target and the gate
// would never fire. The target ask that round 1 fixed is never even posed.
func TestBotDeclinesWhirlerGrantWithoutOwnCreature(t *testing.T) {
	e, whirler := whirlerRogueBoard(t, false)
	opt := findWhirlerAbilityOption(t, e, whirler)
	// Precondition: the ability option carries the polarity signal the gate
	// reads. A nil/empty GrantStatics would make the decline vacuous.
	if statics := opt.GrantStatics; len(statics) != 1 || statics[0] != "CantBlockBy" {
		t.Fatalf("precondition: ability option GrantStatics = %v, want [CantBlockBy]", statics)
	}
	pass := passIdx(t, e)
	in := newTestBot(1).answer(e, e.Pending())
	if len(in.Choices) != 1 || in.Choices[0] != pass {
		t.Fatalf("bot answered %v, want the pass option (%d): a boon grant must not be activated with no own creature",
			in.Choices, pass)
	}
}

// TestBotWhirlerGrantAimsOwnCreature is the fb-20260927T153930Z regression,
// the TARGET half, through the real ability flow: activating Whirler Rogue
// pays its cost and poses the target ask, and the bot's answer there must be
// its OWN creature -- never the enemy's. The grant then really resolves onto
// the own creature (Whirler Rogue remembers it), so the assertion covers
// activation, targeting and resolution together.
func TestBotWhirlerGrantAimsOwnCreature(t *testing.T) {
	e, whirler := whirlerRogueBoard(t, true)
	own := e.G.Zone(state.ZBattlefield, 0)[len(e.G.Zone(state.ZBattlefield, 0))-1]
	if e.G.Obj(own).Face().Name != "Own Giant" {
		t.Fatalf("fixture precondition: last own battlefield card is %q, want Own Giant", e.G.Obj(own).Face().Name)
	}
	// Precondition: the enemy creature really is offered as a target, so a
	// "picked own" answer is not the all-own fallback passing by accident.
	submitChoices(t, e, activateIndex(t, e, whirler))
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("fixture precondition: decision after activation = %+v, want KTarget", d)
	}
	haveEnemy, haveOwn := false, false
	for _, o := range d.Options {
		if o.Obj == own {
			haveOwn = true
		}
		if o.Controller == 1 {
			haveEnemy = true
		}
	}
	if !haveOwn || !haveEnemy {
		t.Fatalf("fixture precondition: target options %v missing own (%d) or enemy", d.Options, own)
	}
	in := newTestBot(1).answer(e, d)
	if len(in.Choices) != 1 {
		t.Fatalf("grant target choice = %v, want exactly one", in.Choices)
	}
	if obj := d.Options[in.Choices[0]].Obj; obj != own {
		t.Fatalf("grant aimed at %+v, want the seat's own creature (%d)", d.Options[in.Choices[0]], own)
	}
	submitChoices(t, e, in.Choices...)
	// Drain resolution with the bot, bounded: cost payment and stack
	// resolution may pose additional asks, and every one gets the policy's
	// own answer rather than a hand-written fallback.
	for i := 0; i < 40 && len(e.G.Stack) > 0; i++ {
		d := e.Pending()
		if d == nil {
			e.Advance()
			d = e.Pending()
			if d == nil {
				break
			}
		}
		in := newTestBot(1).answer(e, d)
		if err := e.Submit(in); err != nil {
			t.Fatalf("drain submit %d: %v", i, err)
		}
	}
	if len(e.G.Stack) > 0 {
		t.Fatalf("stack did not drain (depth %d)", len(e.G.Stack))
	}
	// The grant is real: Whirler Rogue's registered CantBlockBy restriction
	// exists, remembers the OWN creature, and has expired at cleanup.
	found := false
	for _, ce := range e.active() {
		if ce.Restriction == "CantBlockBy" && ce.Source == whirler && len(ce.Remembered) == 1 && ce.Remembered[0] == own {
			found = true
		}
	}
	if !found {
		t.Fatal("the CantBlockBy grant did not resolve onto the own creature")
	}
}
