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
const xMinOfferGateParamHiSrc = "Name:XMin Offer Gate ParamHi\nManaCost:0\nTypes:Artifact\n" +
	"A:AB$ GainLife | Cost$ XMin3 X | XMin$ 4 | Defined$ You | LifeAmount$ 2 | SpellDescription$ x.\nOracle:x\n"
const xMinOfferGateMalformedSrc = "Name:XMin Offer Gate Malformed\nManaCost:0\nTypes:Artifact\n" +
	"A:AB$ GainLife | Cost$ X | XMin$ two | Defined$ You | LifeAmount$ 2 | SpellDescription$ x.\nOracle:x\n"
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

// TestXMinParamOfferGateKeepsHigherCostFloor pins the max fold at the offer:
// the parameter floor 4 is above the cost's XMin3 token and must withhold the
// activation until the fourth generic mana is available.
func TestXMinParamOfferGateKeepsHigherCostFloor(t *testing.T) {
	e, _, _ := newFixtureDeck(t, 97, xMinOfferGateParamHiSrc, xMinOfferGateBearSrc)
	relicID := moveSeeded(t, e, 0, xMinOfferGateParamHiSrc, state.ZBattlefield)
	e.pending = nil
	e.Advance()
	xMinOfferGatePrecondition(t, e, relicID, "4", 3, 1)
	for range 3 {
		addMana(t, e, 0, "C")
	}
	if _, ok := findAbilityOption(e, relicID, 0); ok {
		t.Fatal("activation offered with three mana: ability XMin$ 4 must raise the cost's XMin3 floor")
	}
	addMana(t, e, 0, "C")
	opt, ok := findAbilityOption(e, relicID, 0)
	if !ok {
		t.Fatalf("activation not offered with four generic mana: %+v", e.Pending().Options)
	}
	submitChoices(t, e, opt.Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Prompt != "Choose a value for X" {
		t.Fatalf("X ask = %+v, want the announcement decision", d)
	}
	if len(d.Options) == 0 || d.Options[0].Kind != "x" || d.Options[0].Amount != 4 {
		t.Fatalf("X options = %+v, want range to start at 4", d.Options)
	}
	for _, o := range d.Options {
		if o.Kind != "x" || o.Amount < 4 {
			t.Fatalf("X options = %+v, want none below 4", d.Options)
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

	// Parsing validity must affect the actual offer: an invalid parameter
	// leaves the one-mana X offer available, while XMin$ 2 with the identical
	// X-only cost withholds it. This assertion fails if the offer stops using
	// the shared parser/floor.
	badEngine, _, _ := newFixtureDeck(t, 98, xMinOfferGateMalformedSrc, xMinOfferGateBearSrc)
	badID := moveSeeded(t, badEngine, 0, xMinOfferGateMalformedSrc, state.ZBattlefield)
	badEngine.pending = nil
	badEngine.Advance()
	xMinOfferGatePrecondition(t, badEngine, badID, "two", 0, 1)
	addMana(t, badEngine, 0, "C")
	if _, ok := findAbilityOption(badEngine, badID, 0); !ok {
		t.Fatal("invalid XMin$ two unexpectedly withheld the one-mana X offer")
	}

	goodEngine, _, _ := newFixtureDeck(t, 99, xMinOfferGateSrc, xMinOfferGateBearSrc)
	goodID := moveSeeded(t, goodEngine, 0, xMinOfferGateSrc, state.ZBattlefield)
	goodEngine.pending = nil
	goodEngine.Advance()
	xMinOfferGatePrecondition(t, goodEngine, goodID, "2", 0, 1)
	addMana(t, goodEngine, 0, "C")
	if _, ok := findAbilityOption(goodEngine, goodID, 0); ok {
		t.Fatal("valid XMin$ 2 failed to withhold the same one-mana X offer")
	}
}
