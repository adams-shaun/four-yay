package rules

// cantsac1: a CantSacrifice static's ForCost$ True is enforced on the COST
// path (rules/layers.go sacrificeBlocked / sacrificeBlockedForCost), and its
// ValidCause$ is evaluated there against the pending cast/activation
// (rules/trigger_cause.go causeCostAdmits) instead of being skipped whole.
//
// vc-static1 whitelisted ForCost$/ValidCause$ but deliberately skipped every
// ForCost$ True line (the cost call sites' pending cast/activation identity
// was not modelled), so Angel of Jubilation and Yasharn, Implacable Earth --
// the two corpus CantSacrifice carriers of
// `ValidCause$ Spell,Activated | ForCost$ True` -- were inert on the cost
// path: a creature could be sacrificed to pay a spell's or ability's cost.
//
// Every leaf drives a REAL corpus card through the ordinary offer/payment
// paths and ends replay-verified. Each leaf first asserts its own
// precondition (the carrier and the candidate are on the battlefield, and
// the control WITHOUT the restriction DOES offer the payment), so a vacuous
// board fails loudly rather than passing silently.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// cantSacForCostCard looks a corpus card up and fails fast when it is absent.
func cantSacForCostCard(t *testing.T, reg *cards.Registry, name string) *cards.Card {
	t.Helper()
	c, ok := reg.Lookup(name)
	if !ok {
		t.Fatalf("corpus missing %s", name)
	}
	return c
}

// battlefieldObj returns the id of seat p's copy of c (matched by card
// pointer, the bearOnBoard contract) and fails when it is not on the
// battlefield -- the precondition every leaf below depends on.
func battlefieldObj(t *testing.T, e *Engine, p state.PlayerID, c *cards.Card) state.ObjID {
	t.Helper()
	for _, id := range e.G.Zone(state.ZBattlefield, p) {
		if o := e.G.Obj(id); o != nil && o.Card == c {
			return id
		}
	}
	t.Fatalf("card %s is not on seat %d's battlefield", c.Faces[0].Name, p)
	return 0
}

// hasCastOptionFor reports whether the pending priority decision offers a
// "cast" option naming id.
func hasCastOptionFor(e *Engine, id state.ObjID) bool {
	d := e.Pending()
	if d == nil {
		return false
	}
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == id {
			return true
		}
	}
	return false
}

// TestCantSacrificeForCostWithholdsActivatedAbilityCost is the filing
// symptom: with Angel of Jubilation on the battlefield, Viscera Seer's
// `Cost$ Sac<1/Creature>` activated ability (the real castable ->
// nonManaCastable Sac walk, and the sacAsk payment path) has no legal
// candidate, because Angel's `ValidCard$ Creature | ValidCause$
// Spell,Activated | ForCost$ True` static now reads the cost provenance and
// blocks every creature. The control leaf (no Angel) proves the ability is
// otherwise offered, so the absence is the restriction's doing.
func TestCantSacrificeForCostWithholdsActivatedAbilityCost(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	angel := cantSacForCostCard(t, reg, "Angel of Jubilation")
	seer := cantSacForCostCard(t, reg, "Viscera Seer")
	bear := card(t, staticBearFixture)

	// Control: the same board WITHOUT Angel still offers the ability, so the
	// restricted leaf's absence cannot be a board/vehicle defect.
	ctl, ctlCfg := restrictionGame(t, 9401,
		[][]*cards.Card{{}, {}},
		[][]*cards.Card{{seer, bear}, {}})
	ctlSeer := battlefieldObj(t, ctl, 0, seer)
	ctlBear := battlefieldObj(t, ctl, 0, bear)
	if ctlSeer == ctlBear {
		t.Fatal("precondition: the vehicle and the candidate must be distinct")
	}
	addMana(t, ctl, 0, "")
	if _, ok := findAbilityOption(ctl, ctlSeer, 0); !ok {
		t.Fatalf("control without Angel: Viscera Seer's Sac ability is not offered: %+v", ctl.Pending())
	}
	replayCheck(t, ctl, ctlCfg)

	// Restricted: Angel + the same vehicle + candidate. Every creature is
	// blocked, so the ability has zero candidates and must not be offered.
	e, cfg := restrictionGame(t, 9402,
		[][]*cards.Card{{}, {}},
		[][]*cards.Card{{angel, seer, bear}, {}})
	angelID := battlefieldObj(t, e, 0, angel)
	seerID := battlefieldObj(t, e, 0, seer)
	bearID := battlefieldObj(t, e, 0, bear)
	if e.G.Obj(bearID).Zone != state.ZBattlefield || e.G.Obj(angelID).Zone != state.ZBattlefield {
		t.Fatal("precondition: the carrier and the candidate must be on the battlefield")
	}
	if !e.sacrificeBlockedForCost(bearID, costCauseActivated) {
		t.Fatal("SacrificeBlocked must block the candidate for an activated-ability cost")
	}
	addMana(t, e, 0, "")
	if opt, ok := findAbilityOption(e, seerID, 0); ok {
		t.Fatalf("Angel of Jubilation must block the Sac cost of Viscera Seer's ability; offered %+v", opt)
	}
	replayCheck(t, e, cfg)
}

// TestCantSacrificeForCostWithholdsSpellCost is the spell half of the same
// gate: Village Rites' `Cost$ B Sac<1/Creature>` additional cost runs through
// castable -> nonManaCastable with ability=false, so the cause is Spell. The
// control leaf proves the cast is otherwise offered.
func TestCantSacrificeForCostWithholdsSpellCost(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	angel := cantSacForCostCard(t, reg, "Angel of Jubilation")
	rites := cantSacForCostCard(t, reg, "Village Rites")
	bear := card(t, staticBearFixture)

	// Control: no Angel; the bear pays the additional cost, so the cast is
	// offered as soon as the {B} is funded.
	ctl, ctlCfg := restrictionGame(t, 9403,
		[][]*cards.Card{{rites}, {}},
		[][]*cards.Card{{bear}, {}})
	ctlBear := battlefieldObj(t, ctl, 0, bear)
	if ctlBear == 0 {
		t.Fatal("precondition: the sacrifice candidate must exist")
	}
	ctlRites := findAndMoveToHand(t, ctl, 0, "Village Rites")
	addMana(t, ctl, 0, "B")
	if !hasCastOptionFor(ctl, ctlRites) {
		t.Fatalf("control without Angel: Village Rites is not offered: %+v", ctl.Pending())
	}
	replayCheck(t, ctl, ctlCfg)

	// Restricted: with Angel every creature is blocked, so the additional
	// Sac cost has no candidate and the cast must be withheld.
	e, cfg := restrictionGame(t, 9404,
		[][]*cards.Card{{rites}, {}},
		[][]*cards.Card{{angel, bear}, {}})
	bearID := battlefieldObj(t, e, 0, bear)
	angelID := battlefieldObj(t, e, 0, angel)
	if e.G.Obj(bearID).Zone != state.ZBattlefield || e.G.Obj(angelID).Zone != state.ZBattlefield {
		t.Fatal("precondition: the carrier and the candidate must be on the battlefield")
	}
	if !e.sacrificeBlockedForCost(bearID, costCauseSpell) {
		t.Fatal("SacrificeBlocked must block the candidate for a spell cost")
	}
	ritesID := findAndMoveToHand(t, e, 0, "Village Rites")
	addMana(t, e, 0, "B")
	if hasCastOptionFor(e, ritesID) {
		t.Fatalf("Angel of Jubilation must withhold Village Rites' Sac cost: %+v", castOptions(t, e))
	}
	replayCheck(t, e, cfg)
}

// TestCantSacrificeForCostLeavesEffectSacrificeAlone proves the ForCost$
// discrimination is real: the SAME Angel of Jubilation static must NOT block
// a sacrifice an EFFECT causes. Fleshbag Marauder's ETB ("each player
// sacrifices a creature") resolves through effSacrifice with forCost=false,
// so the bear stays a legal candidate and is taken.
func TestCantSacrificeForCostLeavesEffectSacrificeAlone(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	angel := cantSacForCostCard(t, reg, "Angel of Jubilation")
	fleshbag := cantSacForCostCard(t, reg, "Fleshbag Marauder")
	bear := card(t, staticBearFixture)

	e, cfg := restrictionGame(t, 9405,
		[][]*cards.Card{{fleshbag}, {}},
		[][]*cards.Card{{angel, bear}, {}})
	bearID := battlefieldObj(t, e, 0, bear)
	angelID := battlefieldObj(t, e, 0, angel)
	if e.G.Obj(bearID).Zone != state.ZBattlefield || e.G.Obj(angelID).Zone != state.ZBattlefield {
		t.Fatal("precondition: the carrier and the candidate must be on the battlefield")
	}
	// The same static MUST block this bear on the COST path: without this the
	// leaf could pass with the restriction entirely inert, and it pins the
	// cost/effect discrimination from both sides.
	if !e.sacrificeBlockedForCost(bearID, costCauseActivated) {
		t.Fatal("precondition: Angel's ForCost$ True static is not active on the cost path")
	}
	fleshID := findAndMoveToHand(t, e, 0, "Fleshbag Marauder")
	addMana(t, e, 0, "CCB")
	castFromPriority(t, e, fleshID)
	passPriorityUntilAsk(t, e, "Sacrifice")
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "sacrifice" {
		t.Fatalf("expected Fleshbag Marauder's ETB sacrifice ask, got %+v", d)
	}
	if len(tokenOptionsFor(d, bearID)) != 1 {
		t.Fatalf("an EFFECT-caused sacrifice must still offer the bear under Angel's ForCost$ True: %+v", d.Options)
	}
	chooseSacrificeAnswer(t, e, d, bearID)
	passUntilStackEmpty(t, e, 60)
	if z := e.G.Obj(bearID).Zone; z == state.ZBattlefield {
		t.Fatal("the effect-caused sacrifice of the bear was blocked: ForCost$ True leaked onto the effect path")
	}
	if z := e.G.Obj(angelID).Zone; z != state.ZBattlefield {
		t.Fatalf("Angel of Jubilation left the battlefield: %s", z)
	}
	replayCheck(t, e, cfg)
}
