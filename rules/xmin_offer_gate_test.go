package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// Task cost:xmin-param-offer-gate: the ability-level `XMin$ N` parameter
// must gate the ACTIVATION OFFER, not only the X ask. Before this ticket the
// offer's cheapest legal announced-X price (costMods.feasibleAny's bound
// pricing) read only the cost-embedded XMin<N> token, so an activated
// ability with `Cost$ X | XMin$ 2` was offered with one generic mana in the
// pool; choosing it led to an X ask whose legal range was empty and the
// activation aborted -- an erroneous offer, not a livelock. The offer gate
// now raises the cost's own XMin bound by the same parsed ability parameter
// xAsk folds (xMinAbilityParam), so the offer and the ask share one floor.

// xMinOfferGateSrc is a bare-`X` printed mana cost with NO XMin<N> cost
// token -- the parameter is the only carrier of the floor. GainLife keeps
// the ability target-free, exactly the xmin_param_test.go fixture shape.
const xMinOfferGateSrc = "Name:XMin Offer Gate\nManaCost:0\nTypes:Artifact\n" +
	"A:AB$ GainLife | Cost$ X | XMin$ 2 | Defined$ You | LifeAmount$ 2 | SpellDescription$ x.\nOracle:x\n"
const xMinOfferGateCostHiSrc = "Name:XMin Offer Gate CostHi\nManaCost:0\nTypes:Artifact\n" +
	"A:AB$ GainLife | Cost$ XMin3 X | XMin$ 1 | Defined$ You | LifeAmount$ 2 | SpellDescription$ x.\nOracle:x\n"
const xMinOfferGateBearSrc = "Name:Bear\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"

// xMinOfferGatePrecondition asserts the fixture really carries the parameter
// and a floor-FREE cost (XMin token absent, one printed {X} pip), so the
// gate assertions below exercise the ability parameter and not a vacuous
// fixture. It also asserts the source is on the battlefield, the zone the
// offer loop reads activated abilities from.
func xMinOfferGatePrecondition(t *testing.T, e *Engine, srcID state.ObjID, wantParam string, wantCostXMin int32, wantCostX int) {
	t.Helper()
	o := e.G.Obj(srcID)
	if o == nil || o.Zone != state.ZBattlefield || o.Face() == nil || len(o.Face().Abilities) != 1 {
		t.Fatalf("fixture source = %+v, want one ability on a battlefield face", o)
	}
	ab := o.Face().Abilities[0]
	if ab.Params["XMin"] != wantParam {
		t.Fatalf("fixture ability XMin param = %q, want %q", ab.Params["XMin"], wantParam)
	}
	c := ParseCost(ab.Params["Cost"])
	if c.XMin != wantCostXMin || c.X != wantCostX {
		t.Fatalf("fixture cost = XMin %d X %d, want XMin %d X %d (the parameter, not the cost token, must carry the floor)",
			c.XMin, c.X, wantCostXMin, wantCostX)
	}
}

// TestXMinParamOfferGateWithholdsUnpayableMinimum is the brief's regression:
// with only one generic mana (paying X=1 but never X=2) the activation
// option is WITHHELD; after one more generic mana it is offered, its X ask
// starts at 2, and paying X=2 actually resolves the ability.
func TestXMinParamOfferGateWithholdsUnpayableMinimum(t *testing.T) {
	e, cfg, _ := newFixtureDeck(t, 96, xMinOfferGateSrc, xMinOfferGateBearSrc)
	relicID := moveSeeded(t, e, 0, xMinOfferGateSrc, state.ZBattlefield)
	e.pending = nil
	e.Advance()
	xMinOfferGatePrecondition(t, e, relicID, "2", 0, 1)

	// One generic mana pays X=1 but not the XMin$ 2 floor: no activation
	// option. The priority decision itself must still be live -- the absence
	// is the gate withholding one option, not an empty menu.
	addMana(t, e, 0, "C")
	if d := e.Pending(); d == nil || d.Kind != decision.KPriority {
		t.Fatalf("priority decision after funding = %+v, want a live priority ask", d)
	}
	if _, ok := findAbilityOption(e, relicID, 0); ok {
		t.Fatal("activation offered with one generic mana: XMin$ 2 needs two, the offer must be withheld")
	}
	// One more generic mana makes the smallest legal announcement (X=2)
	// payable: the option appears.
	addMana(t, e, 0, "C")
	opt, ok := findAbilityOption(e, relicID, 0)
	if !ok {
		t.Fatalf("activation not offered with two generic mana: %+v", e.Pending().Options)
	}
	submitChoices(t, e, opt.Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Prompt != "Choose a value for X" {
		t.Fatalf("X ask = %+v, want the announcement decision", d)
	}
	for _, o := range d.Options {
		if o.Kind == "x" && o.Amount < 2 {
			t.Fatalf("X options = %+v, want none below 2: the XMin$ 2 parameter floors the ask too", d.Options)
		}
	}
	if len(d.Options) == 0 || d.Options[0].Kind != "x" || d.Options[0].Amount != 2 {
		t.Fatalf("X options = %+v, want the range to START at X = 2", d.Options)
	}
	startLife := e.G.Players[0].Life
	submitChoices(t, e, d.Options[0].Index)
	passUntilStackEmpty(t, e, 20)
	if got := e.G.Players[0].Life; got != startLife+2 {
		t.Fatalf("life after paying X=2 = %d, want %d (the activation actually settled at the floor)", got, startLife+2)
	}
	replayCheck(t, e, cfg)
}

// TestXMinParamOfferGateKeepsHigherCostFloor pins the offer path's fold
// direction that TestXMinParamDoesNotLowerCostFloor pins for the ask: a cost
// whose own XMin3 token floors ABOVE the ability's XMin$ 1 is offered only
// when X=3 is payable -- the parameter must not lower the cost's bound at
// the offer either.
func TestXMinParamOfferGateKeepsHigherCostFloor(t *testing.T) {
	e, _, _ := newFixtureDeck(t, 97, xMinOfferGateCostHiSrc, xMinOfferGateBearSrc)
	relicID := moveSeeded(t, e, 0, xMinOfferGateCostHiSrc, state.ZBattlefield)
	e.pending = nil
	e.Advance()
	xMinOfferGatePrecondition(t, e, relicID, "1", 3, 1)

	// Two generic mana pay X=2 but not the cost's own XMin3 floor: withheld.
	addMana(t, e, 0, "C")
	addMana(t, e, 0, "C")
	if _, ok := findAbilityOption(e, relicID, 0); ok {
		t.Fatal("activation offered on two generic mana: the cost's XMin3 token must keep the higher floor at the offer")
	}
	// The third generic makes X=3 payable: offered, and the ask starts at 3.
	addMana(t, e, 0, "C")
	opt, ok := findAbilityOption(e, relicID, 0)
	if !ok {
		t.Fatalf("activation not offered on three generic mana: %+v", e.Pending().Options)
	}
	submitChoices(t, e, opt.Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Prompt != "Choose a value for X" {
		t.Fatalf("X ask = %+v, want the announcement decision", d)
	}
	for _, o := range d.Options {
		if o.Kind == "x" && o.Amount < 3 {
			t.Fatalf("X options = %+v, want none below 3: the cost's XMin3 token floors the ask", d.Options)
		}
	}
}

// A corpus-independent pin of the shared parse: a zero, negative or
// non-numeric XMin$ parameter binds nothing, the exact fallback xAsk always
// took, so a malformed corpus line cannot invent a floor at either reader.
func TestXMinAbilityParamParsesOnlyPositiveIntegers(t *testing.T) {
	ab := &cards.SA{API: "GainLife", Params: map[string]string{}}
	if got := xMinAbilityParam(ab); got != 0 {
		t.Fatalf("absent XMin$ param parsed %d, want 0", got)
	}
	ab.Params["XMin"] = "2"
	if got := xMinAbilityParam(ab); got != 2 {
		t.Fatalf("XMin$ 2 parsed %d, want 2", got)
	}
	for _, bad := range []string{"0", "-1", "two", "2x", "99999999999999999999"} {
		ab.Params["XMin"] = bad
		if got := xMinAbilityParam(ab); got != 0 {
			t.Fatalf("XMin$ %q parsed %d, want 0 (binds nothing)", bad, got)
		}
	}
}
