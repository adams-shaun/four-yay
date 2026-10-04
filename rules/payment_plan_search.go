package rules

import (
	"fmt"
	"reflect"
	"slices"

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

// paymentPlanQueryUnits is paymentPlanManaUnits, computed once per query
// scope and player. The returned units are shared: callers only read them.
func (e *Engine) paymentPlanQueryUnits(p state.PlayerID) []windowManaUnit {
	q := e.PlanQuery
	if !q.Valid(e.L) {
		return e.paymentPlanManaUnits(p)
	}
	if units, ok := q.Units[p]; ok {
		if walkCacheVerify && !pay.SameUnits(units, e.paymentPlanManaUnits(p)) {
			panic(fmt.Sprintf("payment plan query: cached source census for player %d is stale", p))
		}
		return units
	}
	units := e.paymentPlanManaUnits(p)
	if q.Units == nil {
		q.Units = map[state.PlayerID][]windowManaUnit{}
	}
	q.Units[p] = units
	return units
}

// paymentPlanQueryAlternatives is paymentPlanUnitAlternatives, computed once
// per query scope and source. The returned alternatives are shared: callers
// only read them.
func (e *Engine) paymentPlanQueryAlternatives(u windowManaUnit) []pay.Alt {
	q := e.PlanQuery
	if !q.Valid(e.L) {
		return e.paymentPlanUnitAlternatives(u)
	}
	if alts, ok := q.Alts[u.ID]; ok {
		if walkCacheVerify && !slices.EqualFunc(alts, e.paymentPlanUnitAlternatives(u), pay.PlanSameAlternative) {
			panic(fmt.Sprintf("payment plan query: cached alternatives for source %d are stale", u.ID))
		}
		return alts
	}
	// The scope's alternatives share one arena, reset when the scope is
	// recycled (paymentPlanQueryBegin): no alternative outlives its scope.
	var alts []pay.Alt
	q.AltArena, alts = e.appendUnitAlternatives(q.AltArena, u)
	if q.Alts == nil {
		q.Alts = map[state.ObjID][]pay.Alt{}
	}
	q.Alts[u.ID] = alts
	return alts
}

// paymentPlanQueryClasses is paymentPlanClasses over one phase's choices,
// computed once per query scope, payer and phase: within a scope the
// choices are the cached census's, so every candidate cast groups them the
// same way. The classes are shared: the search only reads them.
func (e *Engine) paymentPlanQueryClasses(p state.PlayerID, minTier pay.Tier, choices [][]pay.Alt) []pay.Class {
	q := e.PlanQuery
	if !q.Valid(e.L) {
		return pay.Classes(choices)
	}
	key := pay.PlanClassesKey{Payer: p, MinTier: minTier}
	if classes, ok := q.Classes[key]; ok {
		if walkCacheVerify && !reflect.DeepEqual(pay.PlanClassMembers(classes), pay.PlanClassMembers(pay.Classes(choices))) {
			panic(fmt.Sprintf("payment plan query: cached classes for player %d are stale", p))
		}
		return classes
	}
	classes := pay.Classes(choices)
	if q.Classes == nil {
		q.Classes = map[pay.PlanClassesKey][]pay.Class{}
	}
	q.Classes[key] = classes
	return classes
}

// paymentSearchEnv wires the pay package's search to the engine's mana
// solver: a complete count vector is settled with resolveManaWith, the same
// solver execution uses.
func paymentSearchEnv() pay.Env {
	return pay.Env{
		Settle: func(c Cost, pool state.Mana, life int32) (state.Mana, bool) {
			paid, ok := resolveManaWith(c, pool, state.Mana{}, [7]state.Mana{}, life, false, pipRider{}, nil)
			return paid.Pool, ok
		},
		Verify: walkCacheVerify,
	}
}
