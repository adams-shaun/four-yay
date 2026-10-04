package rules

import (
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
	return (*Engine)(v).matchesSpec(spec, id, sc)
}

func (v *payEval) WindowUnits(p state.PlayerID) []pay.WindowUnit {
	return (*Engine)(v).windowManaUnits(p)
}
