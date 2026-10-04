package pay

import (
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// PlanCostKey is a query scope's planner memo key: everything a
// plain mana cost's plan reads besides the scope's state.
type PlanCostKey struct {
	Payer   state.PlayerID
	Colored state.Mana
	Generic int32
	Demand  [5]int
}

// PlanCostMemo is one memoised planner outcome (its own plan copy).
type PlanCostMemo struct {
	Key PlanCostKey
	Out PlanOutcome
}

// PlanQuery is the per-query scratch of one planner query, or of one
// PaymentActionsForPriority build around every candidate's query: the
// zone-entry index, each player's source census (paymentPlanManaUnits) and
// each source's alternatives (paymentPlanUnitAlternatives), which depend on
// the board and the payer but never on the cast. It is
// installed for the query's duration and every read checks it still
// describes the engine's log; Clone copies none of it.
type PlanQuery struct {
	// LogLen / logBase pin the log the scope describes (valid).
	LogLen  int
	LogBase *events.Event
	// Payer is the player a kept scope was built for
	// (paymentPlanQueryKeep).
	Payer state.PlayerID
	// Owner is the session of the engine that made the scope (paymentPlanQueryBegin's
	// recycling never takes another engine's).
	Owner   *Session
	Units   map[state.PlayerID][]WindowUnit
	Alts    map[state.ObjID][]Alt
	Classes map[PlanClassesKey][]Class
	// AltArena backs alts' lists.
	AltArena []Alt
	// Plans memoises plain-cost planner outcomes (planPaymentCost).
	Plans []PlanCostMemo
	// Installs counts the scope's current installations (Begin, Resume);
	// only an uninstalled scope is recycled.
	Installs int
	// SpendReaderOut caches paymentPlanBoardSpendReaderOut: 0 unread, 1
	// false, 2 true.
	SpendReaderOut uint8
}

type PlanClassesKey struct {
	Payer   state.PlayerID
	MinTier Tier
}

func (q *PlanQuery) Valid(l *events.Log) bool {
	if q == nil || len(l.Events) != q.LogLen {
		return false
	}
	return q.LogLen == 0 || &l.Events[0] == q.LogBase
}

// QueryTok is one paymentPlanQueryBegin's undo: the scope it
// installed (nil when an enclosing valid scope was reused) and the one it
// replaced. End hands a scope installed nowhere else and not kept back to
// the engine's free slot (paymentPlanQueryRecycle).
type QueryTok struct {
	Q, Prev *PlanQuery
}
