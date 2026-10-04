package rules

import (
	"fmt"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/rules/pay"
	"github.com/adams-shaun/gorge/state"
)

// pay_eval.go is package rules' side of pay.Eval (lasagna spec §9.2): the
// evaluations the payment layer runs with the engine as the effects.Host.
// payEval is the Engine under another method set (a pointer conversion, no
// allocation); each method is the engine call its caller made before it
// moved.
type payEval Engine

var _ pay.Eval = (*payEval)(nil)

// Eval (pay.Engine) is the engine's evaluation seam.
func (pe *payer) Eval() pay.Eval { return (*payEval)(pe) }

func (v *payEval) EvalCount(ctx *effects.Ctx, expr string) (int32, bool) {
	return effects.EvalCountOK((*Engine)(v), ctx, expr)
}

func (v *payEval) MatchesSpec(spec string, id state.ObjID, sc effects.SpecContext) bool {
	e := (*Engine)(v)
	return e.matchesSpec(spec, id, sc)
}

func (v *payEval) WindowUnits(p state.PlayerID) []pay.WindowUnit {
	e := (*Engine)(v)
	return e.windowManaUnits(p)
}

// paySession is pay.Session under an unexported name, so the embedded field
// does not export the payment state from the Engine.
type paySession = pay.Session

// Session (pay.Engine) is the engine's payment-layer state.
func (pe *payer) Session() *pay.Session { return &pe.paySession }

// ManaShape (pay.Eval) is ab's configured payment-plan shape verdict: a
// configured ability without a SubAbility$ carries it in its mana facts
// (manaSAFacts.shape*); known is false otherwise.
func (v *payEval) ManaShape(ab *cards.SA) (pay.Tier, pay.Consequence, string, bool) {
	e := (*Engine)(v)
	f := e.manaFactsOf(ab)
	if f == nil || !f.shapeKnown {
		return 0, pay.Consequence{}, "", false
	}
	if manaSAFactsVerify {
		tier, c, detail, rider := pay.PaymentPlanShapeTierOf(ab, e.parseCost(ab.ParamStr(cards.PKCost)))
		if rider || tier != f.shapeTier || c != f.shapeCons || detail != f.shapeDetail {
			panic(fmt.Sprintf("rules: configured payment shape for %q disagrees with a recompute", ab.Line))
		}
	}
	return f.shapeTier, f.shapeCons, f.shapeDetail, true
}

// ManaStatic (pay.Eval) is ab's census text reads (manaStaticOf).
func (v *payEval) ManaStatic(ab *cards.SA) pay.ManaStatic {
	e := (*Engine)(v)
	return e.manaStaticOf(ab)
}

// SourceInterference (pay.Eval) is the planner's source-interference check
// (paymentPlanSourceInterference): the trigger and replacement matchers that
// can observe tapping id for ma's mana.
func (v *payEval) SourceInterference(id state.ObjID, ma *cards.SA) (pay.Tier, pay.Consequence, string) {
	e := (*Engine)(v)
	return e.paymentPlanSourceInterference(id, ma)
}

// ZoneEntrySeq (pay.Engine) is the zone-entry index's sequence for id
// (zoneEntrySeq).
func (pe *payer) ZoneEntrySeq(id state.ObjID) uint64 {
	e := (*Engine)(pe)
	return e.zoneEntrySeq(id)
}
