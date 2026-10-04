package pay

import (
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/state"
)

// Eval is the payment layer's evaluation seam (lasagna spec §9.2): every
// read that needs the engine as an effects.Host -- a Count$/Amount$
// expression, a ValidCard$ filter under a resolution's spec context, the
// payment window's mana sources -- is one named method here, implemented by
// package rules with the engine as the Host. The payment layer builds
// effects.Ctx values itself (they are plain data) but never holds the Host
// (internal/archtest TestPayHoldsNoHost). The method count is a budgeted
// ratchet (TestPayEvalBudget).
type Eval interface {
	// EvalCount evaluates a count expression in ctx (effects.EvalCountOK):
	// the value and whether the evaluator models the expression.
	EvalCount(ctx *effects.Ctx, expr string) (int32, bool)
	// MatchesSpec evaluates a ValidCard$-style filter against the live
	// object id under sc.
	MatchesSpec(spec string, id state.ObjID, sc effects.SpecContext) bool
	// WindowUnits is the CR 601.2g payment window's mana units for p: every
	// mana ability p could activate right now, one unit per activation.
	WindowUnits(p state.PlayerID) []WindowUnit
}
