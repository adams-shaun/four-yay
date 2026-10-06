package rules

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/rules/pay"
	"github.com/adams-shaun/gorge/state"
)

// paymentPlanBoardSpendReaderOut is the board half of the shape gate's
// mana-spent-reader arm: a battlefield trigger reading a cast's converge
// or total mana spent, a permanent granting sunburst, or a targeted-spend
// reader in any zone. Targeted readers can be instants in hand/library, so
// their potential referent's paid spend must be captured before the reader is
// cast. One query scope scans these sources once instead of once per candidate.
// targetedCastSpendReaderOut scans all live object zones because a
// Targeted$CastTotalManaSpent ability need not be a battlefield permanent: it
// may be a spell in hand that later counters a spell already cast. Capturing
// is needed while that reader remains in a hidden zone (for example, its
// library), so this deliberately covers every zone containing card objects.
// reads is the per-face test: Engine.faceScanHas's memoised
// faceScanTargetedCastSpend bit.
func targetedCastSpendReaderOut(g *state.Game, reads func(*cards.Face) bool) bool {
	for _, p := range g.AliveFrom(0) {
		for z := state.Zone(0); z <= state.ZPlanarDeck; z++ {
			for _, id := range g.Zone(z, p) {
				o := g.Obj(id)
				if o == nil {
					continue
				}
				f := o.Face()
				if f != nil && reads(f) {
					return true
				}
			}
		}
	}
	return false
}

func (e *Engine) paymentPlanBoardSpendReaderOut() bool {
	scan := func() bool {
		return e.triggeredConvergeReaderOut() || e.triggeredCastSpendReaderOut() || e.paymentPlanSunburstGrantOut() || targetedCastSpendReaderOut(e.G, func(f *cards.Face) bool { return e.faceScanHas(f, faceScanTargetedCastSpend) })
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
