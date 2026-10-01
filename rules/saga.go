package rules

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// saga.go implements the Saga mechanic (CR 702.151, Forge's kw:Chapter; 236
// corpus files carry the keyword). A Saga is an enchantment whose chapter
// list (K:Chapter:<N>:<svar1>,...,...) names one sub-ability per chapter:
//
//   - It enters with ONE lore counter (granted by rules' EntryCounterGrants
//     path -- the same every-entry-site convention the planeswalker starting
//     loyalty uses -- placed through a real CounterChange event, so a search
//     putting a Saga onto the battlefield is covered too and the CR 614
//     replacement class sees the placement; task addcounter1/2).
//   - After its controller's draw step (rules/turn.go's draw-step entry) it
//     adds another.
//   - Each lore counter triggers the chapter ability of that number: the
//     counter-th SVar in the chapter list, queued as a delayed-shape trigger
//     (the mint path a Mode$ Phase delayed trigger uses -- the ability is an
//     SVar body, not a face Triggers index, so TriggerPush cannot carry it)
//     and resolved as an ordinary triggered ability.
//   - Once it carries its final chapter counter and that chapter ability has
//     left the stack, it is sacrificed (rules/sba.go's checkSagas -- the
//     CR 704.5v state-based action).
//
// Determinism: the chapter SVar list is the keyword parameter's own left-to-
// right order (cards.SagaChapters, the one parser both sides read); the
// trigger queue is the ordinary APNAP drain, so a chapter ability placed by a
// draw-step counter lands before any player priority exactly like every other
// turn-based trigger.

// chapterSpec is cards.SagaChapters, renamed locally so call sites read the
// same way the other keyword helpers here do.
func chapterSpec(f *cards.Face) (int, []string) { return cards.SagaChapters(f) }

// chapterCount is the face's chapter number, or 0 when this face is not a
// Saga (or the parameter does not parse).
func chapterCount(o *state.Object) int {
	n, _ := chapterSpec(o.Face())
	return n
}

// advanceSagas adds one lore counter to every Saga p controls (CR 702.151a's
// "after your draw step" half). One CounterChange event per Saga, in
// battlefield order, so the chapter triggers queue in the same deterministic
// order.
func (e *Engine) advanceSagas(p state.PlayerID) {
	// The turn-based lore counter is put by the Saga's controller, with no
	// stack cause, so publish the adder for the AddCounter class.
	prevAdder := e.SetCounterAdder(p)
	defer e.SetCounterAdder(prevAdder)
	ids := append([]state.ObjID(nil), e.G.Zone(state.ZBattlefield, p)...)
	for _, id := range ids {
		o := e.G.Obj(id)
		if o == nil || chapterCount(o) == 0 {
			continue
		}
		e.emit(events.Event{Kind: events.CounterChange, Obj: id, Counter: "LORE", Amount: 1})
	}
}

// checkChapterTriggers queues one chapter trigger per lore counter the event
// just placed. Every lore-counter placement is a real CounterChange on the
// LORE kind -- the entry grant (rules' foldEntryMove, task addcounter1/2) and
// the draw-step counter (advanceSagas) alike -- so this is called for exactly
// that one event kind. The MoveZone branch this replaced queued the SAME
// chapter I twice on an entry (the CounterChange the engine now emits queued
// it, and then the MoveZone's own tail check queued it again off the live
// counter the placement had already applied -- which made putTriggersOnStack
// pose a spurious trigger-order ask and left resolveTop an empty stack). A
// CounterChange carrying MORE than one counter (a
// Storyweave-style CounterNum$ 2 or Terra's CounterNum$ 3) crosses several
// chapter thresholds at once, so EVERY newly crossed chapter is queued, in
// ascending order -- CR 702.151b's "each chapter ability triggers when its
// chapter number is reached" is per counter, not per event; queueing only the
// final count's chapter would silently skip the crossed ones. The first
// chapter crossed is the count the object had BEFORE the event: the folded
// count minus the event's own Amount (the live counter read below already
// includes it), floored at 1 -- an entry grant starts from chapter
// 1 by construction. A count beyond the final chapter queues nothing past the
// last chapter (the overflow is the SBA's business, not a chapter ability's).
// The queue entry is the delayed-shape pendingTrigger: DelayedID -1 encodes
// "no registration to remove" (every real registration's ID is a monotonic
// uint32 from DelayedNext, so no DelayedPush this build can mint ever removes
// a real registration by accident), Execute names the chapter SVar, and the
// ability resolves out of the source face's SVar table at push time
// (events.Apply's DelayedPush case).
func (e *Engine) checkChapterTriggers(ev events.Event) {
	if ev.Kind != events.CounterChange || ev.Counter != "LORE" || ev.Amount <= 0 {
		return
	}
	o := e.G.Obj(ev.Obj)
	if o == nil || o.Zone != state.ZBattlefield {
		return
	}
	n, names := chapterSpec(o.Face())
	if n == 0 || len(names) == 0 {
		return
	}
	count := int(o.Counter("LORE"))
	if count < 1 {
		return
	}
	first := 1
	if ev.Kind == events.CounterChange {
		first = count - int(ev.Amount) + 1
		if first < 1 {
			first = 1
		}
	}
	last := count
	if last > n {
		last = n
	}
	if last > len(names) {
		last = len(names)
	}
	for i := first; i <= last; i++ {
		name := names[i-1]
		sa := cards.ResolveSVar(o.Face().SVars, name)
		if sa == nil {
			continue
		}
		e.pendingTriggers = append(e.pendingTriggers, pendingTrigger{
			Source:     ev.Obj,
			Controller: o.Controller,
			Delayed:    true,
			DelayedID:  ^uint32(0),
			Execute:    name,
			SA:         sa,
			Ctx: effects.Ctx{
				Source:     ev.Obj,
				Controller: o.Controller,
			},
		})
	}
}

// checkSagas is the CR 704.5v state-based action: a Saga with a lore counter
// greater than or equal to its final chapter number, whose final chapter
// ability has left the stack, is put into its owner's graveyard. "Left the
// stack" is checked as "no ability object of this Saga is pending or on the
// stack" -- the chapter ability is minted as a stack object with
// Source = the Saga, so the scan below covers both the pending queue and the
// stack. A Saga with no chapter list never triggers this (a script defect,
// not a Saga). Reports whether anything was sacrificed, for the SBA loop's
// fixed-point test. One attempt per Saga per checkStateBased call (tried),
// re-armed on an alive-set shrink like every other pass here, so a
// replacement that permanently blocks the move burns one attempt, not the
// whole pass budget.
func (e *Engine) checkSagas(tried *sbaAttempts) bool {
	changed := false
	// The battlefield snapshot the walk ranges (emit moves objects out
	// of the live zone) lives in the Engine's SBA scratch, taken for the
	// walk so a re-entrant pass allocates its own (sbaIDBuf).
	ids := e.sbaIDBuf
	e.sbaIDBuf = nil
	for _, p := range e.G.AliveFrom(0) {
		ids = append(ids[:0], e.G.Zone(state.ZBattlefield, p)...)
		for _, id := range ids {
			o := e.G.Obj(id)
			if o == nil {
				continue
			}
			n, _ := chapterSpec(o.Face())
			if n == 0 || o.Counter("LORE") < int32(n) {
				continue
			}
			busy := false
			for i := range e.pendingTriggers {
				if e.pendingTriggers[i].Source == id {
					busy = true
					break
				}
			}
			if !busy {
				for _, sid := range e.G.Stack {
					if so := e.G.Obj(sid); so != nil && so.Source == id {
						busy = true
						break
					}
				}
			}
			if busy && !tried.sagas[id] {
				// Deferred on e.pendingTriggers, a runtime input the SBA
				// quiet key (rules/sbaquiet.go) does not cover.
				e.sbaUnquiet = true
			}
			if busy {
				continue
			}
			if tried.sagas[id] {
				tried.skips++
				continue
			}
			markTried(&tried.sagas, id)
			e.emit(events.Event{Kind: events.MoveZone, Obj: id,
				From: state.ZBattlefield, To: state.ZGraveyard, Text: "saga concluded"})
			changed = true
		}
	}
	e.sbaIDBuf = ids[:0]
	return changed
}
