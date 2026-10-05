package rules

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// batchTriggerAlreadyQueued extends the shared action bracket to the two
// aggregate counter modes. The first matching event queues normally; later
// matching recipients accumulate their counter amount and object referent.
func batchTriggerAlreadyQueued(e *Engine, t cards.Trigger, key triggerKey, ev *events.Event) bool {
	mode := t.ModeKind()
	if !e.millBatchOpen || mode != cards.TriggerMilledAll && mode != cards.TriggerTapAll &&
		mode != cards.TriggerUntapAll && mode != cards.TriggerCounterAddedAll &&
		mode != cards.TriggerCounterTypeAddedAll {
		return false
	}
	if e.millBatchIdx == nil {
		e.millBatchIdx = map[triggerKey]int{}
	}
	if idx, ok := e.millBatchIdx[key]; ok {
		entry := &e.millBatchLog[idx]
		if mode == cards.TriggerCounterAddedAll || mode == cards.TriggerCounterTypeAddedAll {
			entry.amount += ev.Amount
		} else {
			entry.amount++
		}
		if ev.Obj != 0 {
			entry.milled = batchAppendTarget(entry.milled, state.Target{Obj: ev.Obj})
		}
		return true
	}
	amount := int32(1)
	if mode == cards.TriggerCounterAddedAll || mode == cards.TriggerCounterTypeAddedAll {
		amount = ev.Amount
	}
	entry := millBatchEntry{key: key, idx: len(e.pendingTriggers), amount: amount}
	if ev.Obj != 0 {
		entry.milled = batchAppendTarget(entry.milled, state.Target{Obj: ev.Obj})
	}
	e.millBatchIdx[key] = len(e.millBatchLog)
	e.millBatchLog = append(e.millBatchLog, entry)
	return false
}
