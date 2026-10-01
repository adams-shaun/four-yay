package rules

import "github.com/adams-shaun/gorge/events"

// BeginLifeLossBatch marks a synchronous operation that can cause several
// players to lose life simultaneously. Effects call it around one resolving
// DealDamage, and combat calls it around a damage pass. LifeLostAll triggers
// are deferred until EndLifeLossBatch so one group queues one trigger.
func (e *Engine) BeginLifeLossBatch() {
	e.lifeLossBatchDepth++
	if e.lifeLossBatchDepth == 1 {
		e.lifeLossBatch = make([]events.Event, 0)
	}
}

// EndLifeLossBatch evaluates LifeLostAll once against the whole simultaneous
// group. Other trigger modes were checked as each event happened, preserving
// their per-player semantics.
func (e *Engine) EndLifeLossBatch() {
	if e.lifeLossBatchDepth == 0 {
		return
	}
	e.lifeLossBatchDepth--
	if e.lifeLossBatchDepth != 0 {
		return
	}
	batch := e.lifeLossBatch
	if len(batch) != 0 {
		e.finishingLifeLossBatch = true
		first := batch[0]
		e.checkFaceTriggers(e, &first, nil, 0, 0, false, false, false)
		e.finishingLifeLossBatch = false
	}
	e.lifeLossBatch = nil
}
