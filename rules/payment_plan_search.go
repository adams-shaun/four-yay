package rules

import (
	"github.com/adams-shaun/gorge/rules/pay"
	"github.com/adams-shaun/gorge/state"
)

// paymentPlanBoardSpendReaderOut is the board half of the shape gate's
// mana-spent-reader arm: a battlefield trigger reading a cast's converge
// or total mana spent, or a permanent granting sunburst. It reads only the
// board, never the cast, so one query scope (the offer build around every
// candidate) scans the battlefield once instead of once per candidate.
func (e *Engine) paymentPlanBoardSpendReaderOut() bool {
	scan := func() bool {
		return e.triggeredConvergeReaderOut() || e.triggeredCastSpendReaderOut() || e.paymentPlanSunburstGrantOut()
	}
	q := e.PlanQuery
	if !q.Valid(e.L) {
		return scan()
	}
	if q.SpendReaderOut == 0 {
		q.SpendReaderOut = 1
		if scan() {
			q.SpendReaderOut = 2
		}
	} else if walkCacheVerify && scan() != (q.SpendReaderOut == 2) {
		panic("payment plan query: cached board mana-spent reader verdict is stale")
	}
	return q.SpendReaderOut == 2
}

// paymentPlanQueryResumeBegin is paymentPlanQueryBegin for a pure reader of p's
// posed priority decision (PotentialPaymentPlans, ValidateCastPayment under
// Submit's validation): when the offer builder's own query scope for p was
// kept at this exact decision state (paymentPlanQueryKeep), it is
// reinstalled, so its source census, alternatives, classes and board
// verdicts -- each a pure read of the board the builder read -- are served
// instead of recomputed. The kept scope is keyed like the potential walk
// cache (potentialStamp: the posed decision, the log length, the registry
// version, the arena size, active()'s rebuild count and p's own pool and
// turn stamp) and is reused only at a top-level read of the posed decision
// (potentialWalkUsable); verify mode (walkCacheVerify) recomputes every
// cached census, alternative list and class grouping on each hit.
func (e *Engine) paymentPlanQueryResumeBegin(p state.PlayerID) pay.QueryTok {
	if e.PlanQuery.Valid(e.L) {
		return pay.QueryTok{}
	}
	k := e.PlanQueryKept
	if k == nil || k.Payer != p || !e.potentialWalkUsable() || !k.Valid(e.L) || e.paymentPlanQueryKeptStamp != e.potentialStampNow(p) {
		return pay.PaymentPlanQueryBegin(asPayer(e))
	}
	k.Installs++
	t := pay.QueryTok{Q: k, Prev: e.PlanQuery}
	e.PlanQuery = k
	return t
}

// paymentPlanQueryKeep records the installed query scope as p's kept scope
// at the posed decision (paymentPlanQueryResumeBegin), when the read is a
// top-level read of a posed priority decision.
func (e *Engine) paymentPlanQueryKeep(p state.PlayerID) {
	q := e.PlanQuery
	if !q.Valid(e.L) || !e.potentialWalkUsable() {
		return
	}
	q.Payer = p
	old := e.PlanQueryKept
	e.PlanQueryKept = q
	e.paymentPlanQueryKeptStamp = e.potentialStampNow(p)
	// The replaced kept scope is finished unless it is still installed.
	if old != q {
		pay.PaymentPlanQueryRecycle(asPayer(e), old)
	}
}
