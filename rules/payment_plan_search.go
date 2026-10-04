package rules

import (
	"fmt"
	"reflect"
	"slices"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/rules/pay"
	"github.com/adams-shaun/gorge/state"
)

// paymentPlanQuery is the per-query scratch of one planner query, or of one
// PaymentActionsForPriority build around every candidate's query: the
// zone-entry index, each player's source census (paymentPlanManaUnits) and
// each source's alternatives (paymentPlanUnitAlternatives), which depend on
// the board and the payer but never on the cast. It is
// installed for the query's duration and every read checks it still
// describes the engine's log; Clone copies none of it.
type paymentPlanQuery struct {
	// logLen / logBase pin the log the scope describes (valid).
	logLen  int
	logBase *events.Event
	// payer is the player a kept scope was built for
	// (paymentPlanQueryKeep).
	payer state.PlayerID
	// owner is the engine that made the scope (paymentPlanQueryBegin's
	// recycling never takes another engine's).
	owner   *Engine
	units   map[state.PlayerID][]windowManaUnit
	alts    map[state.ObjID][]pay.Alt
	classes map[paymentPlanClassesKey][]pay.Class
	// altArena backs alts' lists.
	altArena []pay.Alt
	// plans memoises plain-cost planner outcomes (planPaymentCost).
	plans []paymentPlanCostMemo
	// installs counts the scope's current installations (Begin, Resume);
	// only an uninstalled scope is recycled.
	installs int
	// spendReaderOut caches paymentPlanBoardSpendReaderOut: 0 unread, 1
	// false, 2 true.
	spendReaderOut uint8
}

// paymentPlanBoardSpendReaderOut is the board half of the shape gate's
// mana-spent-reader arm: a battlefield trigger reading a cast's converge
// or total mana spent, or a permanent granting sunburst. It reads only the
// board, never the cast, so one query scope (the offer build around every
// candidate) scans the battlefield once instead of once per candidate.
func (e *Engine) paymentPlanBoardSpendReaderOut() bool {
	scan := func() bool {
		return e.triggeredConvergeReaderOut() || e.triggeredCastSpendReaderOut() || e.paymentPlanSunburstGrantOut()
	}
	q := e.paymentPlanQuery
	if !q.valid(e) {
		return scan()
	}
	if q.spendReaderOut == 0 {
		q.spendReaderOut = 1
		if scan() {
			q.spendReaderOut = 2
		}
	} else if walkCacheVerify && scan() != (q.spendReaderOut == 2) {
		panic("payment plan query: cached board mana-spent reader verdict is stale")
	}
	return q.spendReaderOut == 2
}

type paymentPlanClassesKey struct {
	payer   state.PlayerID
	minTier pay.Tier
}

func (q *paymentPlanQuery) valid(e *Engine) bool {
	if q == nil || len(e.L.Events) != q.logLen {
		return false
	}
	return q.logLen == 0 || &e.L.Events[0] == q.logBase
}

// paymentQueryTok is one paymentPlanQueryBegin's undo: the scope it
// installed (nil when an enclosing valid scope was reused) and the one it
// replaced. End hands a scope installed nowhere else and not kept back to
// the engine's free slot (paymentPlanQueryRecycle).
type paymentQueryTok struct {
	q, prev *paymentPlanQuery
}

// paymentPlanQueryBegin installs a query scope -- a still-valid enclosing
// scope is reused -- and returns its undo for paymentPlanQueryEnd, without
// a closure:
//
//	defer e.paymentPlanQueryEnd(e.paymentPlanQueryBegin())
//
// A scope is pure per-query cache; the one a finished query leaves is reset
// and reused by the next (paymentPlanQueryFree), never while installed,
// kept, or owned by another engine (a by-value Engine copy).
func (e *Engine) paymentPlanQueryBegin() paymentQueryTok {
	if e.paymentPlanQuery.valid(e) {
		return paymentQueryTok{}
	}
	q := e.paymentPlanQueryFree
	if q != nil && q.owner == e {
		e.paymentPlanQueryFree = nil
		units, alts, classes, arena, plans := q.units, q.alts, q.classes, q.altArena, q.plans
		clear(units)
		clear(alts)
		clear(classes)
		clear(arena)
		clear(plans)
		*q = paymentPlanQuery{owner: e, units: units, alts: alts, classes: classes, altArena: arena[:0], plans: plans[:0]}
	} else {
		q = &paymentPlanQuery{owner: e}
	}
	q.logLen = len(e.L.Events)
	if q.logLen > 0 {
		q.logBase = &e.L.Events[0]
	}
	q.installs = 1
	t := paymentQueryTok{q: q, prev: e.paymentPlanQuery}
	e.paymentPlanQuery = q
	return t
}

// paymentPlanQueryEnd undoes one paymentPlanQueryBegin (or
// paymentPlanQueryResumeBegin).
func (e *Engine) paymentPlanQueryEnd(t paymentQueryTok) {
	if t.q == nil {
		return
	}
	e.paymentPlanQuery = t.prev
	t.q.installs--
	if t.q != e.paymentPlanQueryKept {
		e.paymentPlanQueryRecycle(t.q)
	}
}

// paymentPlanQueryRecycle hands a finished scope to the free slot: one this
// engine made, installed nowhere (installs 0) and not kept.
func (e *Engine) paymentPlanQueryRecycle(q *paymentPlanQuery) {
	if q != nil && q.owner == e && q.installs == 0 && q != e.paymentPlanQueryKept {
		e.paymentPlanQueryFree = q
	}
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
func (e *Engine) paymentPlanQueryResumeBegin(p state.PlayerID) paymentQueryTok {
	if e.paymentPlanQuery.valid(e) {
		return paymentQueryTok{}
	}
	k := e.paymentPlanQueryKept
	if k == nil || k.payer != p || !e.potentialWalkUsable() || !k.valid(e) || e.paymentPlanQueryKeptStamp != e.potentialStampNow(p) {
		return e.paymentPlanQueryBegin()
	}
	k.installs++
	t := paymentQueryTok{q: k, prev: e.paymentPlanQuery}
	e.paymentPlanQuery = k
	return t
}

// paymentPlanQueryKeep records the installed query scope as p's kept scope
// at the posed decision (paymentPlanQueryResumeBegin), when the read is a
// top-level read of a posed priority decision.
func (e *Engine) paymentPlanQueryKeep(p state.PlayerID) {
	q := e.paymentPlanQuery
	if !q.valid(e) || !e.potentialWalkUsable() {
		return
	}
	q.payer = p
	old := e.paymentPlanQueryKept
	e.paymentPlanQueryKept = q
	e.paymentPlanQueryKeptStamp = e.potentialStampNow(p)
	// The replaced kept scope is finished unless it is still installed.
	if old != q {
		e.paymentPlanQueryRecycle(old)
	}
}

// paymentPlanQueryUnits is paymentPlanManaUnits, computed once per query
// scope and player. The returned units are shared: callers only read them.
func (e *Engine) paymentPlanQueryUnits(p state.PlayerID) []windowManaUnit {
	q := e.paymentPlanQuery
	if !q.valid(e) {
		return e.paymentPlanManaUnits(p)
	}
	if units, ok := q.units[p]; ok {
		if walkCacheVerify && !pay.SameUnits(units, e.paymentPlanManaUnits(p)) {
			panic(fmt.Sprintf("payment plan query: cached source census for player %d is stale", p))
		}
		return units
	}
	units := e.paymentPlanManaUnits(p)
	if q.units == nil {
		q.units = map[state.PlayerID][]windowManaUnit{}
	}
	q.units[p] = units
	return units
}

// paymentPlanQueryAlternatives is paymentPlanUnitAlternatives, computed once
// per query scope and source. The returned alternatives are shared: callers
// only read them.
func (e *Engine) paymentPlanQueryAlternatives(u windowManaUnit) []pay.Alt {
	q := e.paymentPlanQuery
	if !q.valid(e) {
		return e.paymentPlanUnitAlternatives(u)
	}
	if alts, ok := q.alts[u.ID]; ok {
		if walkCacheVerify && !slices.EqualFunc(alts, e.paymentPlanUnitAlternatives(u), pay.PlanSameAlternative) {
			panic(fmt.Sprintf("payment plan query: cached alternatives for source %d are stale", u.ID))
		}
		return alts
	}
	// The scope's alternatives share one arena, reset when the scope is
	// recycled (paymentPlanQueryBegin): no alternative outlives its scope.
	var alts []pay.Alt
	q.altArena, alts = e.appendUnitAlternatives(q.altArena, u)
	if q.alts == nil {
		q.alts = map[state.ObjID][]pay.Alt{}
	}
	q.alts[u.ID] = alts
	return alts
}

// paymentPlanQueryClasses is paymentPlanClasses over one phase's choices,
// computed once per query scope, payer and phase: within a scope the
// choices are the cached census's, so every candidate cast groups them the
// same way. The classes are shared: the search only reads them.
func (e *Engine) paymentPlanQueryClasses(p state.PlayerID, minTier pay.Tier, choices [][]pay.Alt) []pay.Class {
	q := e.paymentPlanQuery
	if !q.valid(e) {
		return pay.Classes(choices)
	}
	key := paymentPlanClassesKey{payer: p, minTier: minTier}
	if classes, ok := q.classes[key]; ok {
		if walkCacheVerify && !reflect.DeepEqual(pay.PlanClassMembers(classes), pay.PlanClassMembers(pay.Classes(choices))) {
			panic(fmt.Sprintf("payment plan query: cached classes for player %d are stale", p))
		}
		return classes
	}
	classes := pay.Classes(choices)
	if q.classes == nil {
		q.classes = map[paymentPlanClassesKey][]pay.Class{}
	}
	q.classes[key] = classes
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
