package rules

import "github.com/adams-shaun/gorge/rules/pay"

type (
	// PaymentPlanStats is the engine-side auto-pay diagnostics sink
	// (pay.PlanStats).
	PaymentPlanStats = pay.PlanStats
	// PaymentPlanOutcome describes a pure planner query (pay.PlanOutcome).
	PaymentPlanOutcome = pay.PlanOutcome
)

// SetPaymentPlanStats attaches s as this engine's auto-pay diagnostics sink;
// nil (the default) detaches it and costs nothing. The sink belongs to this
// engine alone: Clone does not carry it, and it must not be shared between
// engines driven on different goroutines.
func (e *Engine) SetPaymentPlanStats(s *PaymentPlanStats) { e.PaymentStats = s }

// PaymentPlanStats returns the attached sink, or nil.
func (e *Engine) PaymentPlanStats() *PaymentPlanStats { return e.PaymentStats }
