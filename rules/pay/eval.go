package pay

import (
	"github.com/adams-shaun/gorge/cards"
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
	// ManaShape is a configured mana ability's payment-plan shape verdict
	// from its compiled facts; known is false for an ability whose verdict
	// must be computed (a SubAbility$ chain or a runtime-built ability).
	ManaShape(ab *cards.SA) (tier Tier, c Consequence, detail string, known bool)
	// ManaStatic is the census's reads of ab's own text (its configured
	// facts, or read now for an ability outside the configured set).
	ManaStatic(ab *cards.SA) ManaStatic
	// SourceInterference is the planner's check of the triggers and
	// replacements that can observe tapping id for ma's mana.
	SourceInterference(id state.ObjID, ma *cards.SA) (Tier, Consequence, string)
	// ManaUnits is p's payment-plan source census: one unit per mana
	// ability activation the planner may schedule.
	ManaUnits(p state.PlayerID) []WindowUnit
}
