package rules

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
	"strings"
)

// extraTurnSkipped reports whether a live R:Event$ BeginTurn replacement
// would skip the extra turn `seat` is about to begin (Trouble in Pairs,
// Stranglehold, Ugin's Nexus, Gerrard's Hourglass Pendant; CR 500.7's "that
// player skips it instead" reading of R:Event$ BeginTurn | ExtraTurn$ True |
// Skip$ True). There is no per-turn "would begin" log event to hang
// replacement matching on, so the helper poses a SYNTHETIC
// events.ExtraTurn{Amount: 0, Player: seat} event to the ordinary matcher --
// the ActiveZones$ gate, the ValidPlayer$ read and replacementConditionHolds
// are then the shared ones and cannot drift from the other replacement
// families. The read is pure: it emits nothing, and the caller owns every
// event (including the loud Note for a matched ExtraTurn$ line whose action
// this build does not implement -- Skip$ absent, or a ReplaceWith$ body --
// reported in the second return so the turn proceeds loudly rather than
// being skipped silently).
func (e *Engine) extraTurnSkipped(seat state.PlayerID) (skip, unsupported bool) {
	ev := events.Event{Kind: events.ExtraTurn, Player: seat}
	e.forEachReplacementSource(func(id state.ObjID) {
		f := e.replacementFace(id, ev)
		if f == nil {
			return
		}
		for i := range f.Repls {
			r := &f.Repls[i]
			if r.Event != "BeginTurn" || !e.replacementMatches(*r, id, ev) {
				continue
			}
			if r.Params["Skip"] == "True" && r.With == nil {
				skip = true
			} else {
				unsupported = true
			}
		}
	})
	return skip, unsupported
}

// continueUntapReplacements applies the sole replacement automatically, but
// parks a competition for the untapped permanent's controller. Each Untap
// replacement prevents the original event (and may run its ReplaceWith$), so
// the chosen effect completes the event and the others get no second pass.
// This is the same CR 616.1 affected-player choice as MoveZone, with Untap's
// event-specific applySimpleReplacement semantics.
func (e *Engine) continueUntapReplacements(ev events.Event, matches []replMatch) (events.Event, bool) {
	if len(matches) == 1 {
		return e.applySimpleReplacement(ev, matches[0])
	}
	o := e.G.Obj(ev.Obj)
	if o != nil && int(o.Controller) < len(e.G.Players) && !e.G.Players[o.Controller].Lost {
		e.poseUntapReplacementChoice(ev, matches)
		return ev, true
	}
	// A departed affected player cannot choose; retain the deterministic scan
	// order fallback used by the other replacement competitions.
	return e.applySimpleReplacement(ev, matches[0])
}

// applySimpleReplacement handles events whose replacement prevents the event
// (no ReplaceWith$, the CantHappen form) or replaces it with a body. Untap is
// the important example: an ordinary activated DB$ Untap is outside the
// untap step and consequently does not match ValidStepTurnToController$.
func (e *Engine) applySimpleReplacement(ev events.Event, m replMatch) (events.Event, bool) {
	if m.repl.With != nil {
		e.runReplaceWith(e.replCtx(m, ev), ev.Obj, m.repl.With, nil)
	}
	return ev, true
}

// applyBeginPhaseReplacement skips the phase by emitting the following phase
// entry. The skipped StepChange never enters the log, so replay performs
// exactly the same transition without needing an ephemeral "skipped" bit in
// game state. The skip itself is emitted through the UNGUARDED emit --
// applyingReplacement is false here, outside runReplaceWith -- so a chain of
// skips (one effect skipping untap, another upkeep) keeps skipping: the step
// number strictly increases, so the recursion terminates. The landing step's
// own StepChange is the log entry that exists, and every Phase trigger sees
// exactly the steps that actually happened.
func (e *Engine) applyBeginPhaseReplacement(ev events.Event, m replMatch) (events.Event, bool) {
	if m.repl.With != nil {
		e.runReplaceWith(e.replCtx(m, ev), 0, m.repl.With, nil)
	}
	// Cleanup is never skipped: the turn's 514.1/514.2 work is what makes the
	// next turn begin correctly, and no corpus line names it. Bounding the
	// emission here also bounds the chain recursion.
	if ev.Step < state.StepCleanup {
		e.emit(events.Event{Kind: events.StepChange, Step: ev.Step + 1})
	}
	return ev, true
}

// continuePhaseReplacements processes every replacement applicable to one
// proposed step entry. With several effects the active (affected) player
// chooses which gets the first opportunity under CR 616.1. Choosing an
// optional effect leads to its separate apply/decline ask; declining marks
// only that effect used and continues through the remaining mandatory or
// optional effects instead of bypassing replacement matching on the parked
// StepChange. Applying any effect skips the step and completes this event.
func (e *Engine) continuePhaseReplacements(ev events.Event, candidates []replMatch, used []bool) (events.Event, bool) {
	if used == nil {
		used = make([]bool, len(candidates))
	}
	applicable := e.applicablePhaseReplacements(ev, candidates, used)
	if len(applicable) == 0 {
		return ev, false
	}
	if len(applicable) > 1 && int(e.G.Active) < len(e.G.Players) && !e.G.Players[e.G.Active].Lost {
		e.posePhaseOrderChoice(ev, candidates, used, applicable)
		return ev, true
	}
	i := applicable[0]
	if candidates[i].repl.Params["Optional"] == "True" && int(e.G.Active) < len(e.G.Players) &&
		!e.G.Players[e.G.Active].Lost {
		e.posePhaseOptionalChoice(ev, candidates, used, i)
		return ev, true
	}
	return e.applyBeginPhaseReplacement(ev, candidates[i])
}

func (e *Engine) applicablePhaseReplacements(ev events.Event, candidates []replMatch, used []bool) []int {
	var applicable []int
	for i, m := range candidates {
		if !used[i] && e.replacementMatches(*m.repl, m.id, ev) {
			applicable = append(applicable, i)
		}
	}
	return applicable
}

// finishParkedPhase settles the old step boundary exactly once, then either
// applies the selected skip or enters the original proposed step under a
// guard (all applicable replacements have already had their opportunity).
func (e *Engine) finishParkedPhase(rc replChoice, selected int) {
	if rc.boundary {
		e.finishStepBoundary(rc.leaving, rc.ev.Step)
	}
	if selected >= 0 {
		e.applyBeginPhaseReplacement(rc.ev, rc.cands[selected])
	} else {
		saved := e.applyingReplacement
		e.applyingReplacement = true
		e.emit(rc.ev)
		e.applyingReplacement = saved
	}
	if e.pending == nil {
		e.finishEnteredStep()
	}
}

// resumeParkedPhase continues after an optional replacement was declined.
// It preserves the original boundary ownership while either posing the next
// order/optional ask or completing with the remaining mandatory replacement.
func (e *Engine) resumeParkedPhase(rc replChoice) {
	applicable := e.applicablePhaseReplacements(rc.ev, rc.cands, rc.applied)
	if len(applicable) == 0 {
		e.finishParkedPhase(rc, -1)
		return
	}
	if len(applicable) > 1 && int(e.G.Active) < len(e.G.Players) && !e.G.Players[e.G.Active].Lost {
		rc.kind = replChoicePhaseOrder
		rc.applicable = append(rc.applicable[:0], applicable...)
		e.replChoices = append([]replChoice{rc}, e.replChoices...)
		return
	}
	i := applicable[0]
	if rc.cands[i].repl.Params["Optional"] == "True" && int(e.G.Active) < len(e.G.Players) &&
		!e.G.Players[e.G.Active].Lost {
		rc.kind = replChoicePhaseOptional
		rc.selected = i
		rc.applicable = nil
		e.replChoices = append([]replChoice{rc}, e.replChoices...)
		return
	}
	e.finishParkedPhase(rc, i)
}

// applyTransformReplacement lets every matching "as this transforms" body
// resolve, then leaves the FlipFace event intact. Forge writes these as an
// augmentation (Sephiroth gains its emblem as it becomes the Angel), not as
// a substitute that cancels the transformation.
func (e *Engine) applyTransformReplacement(ev events.Event, matches []replMatch) (events.Event, bool) {
	for _, m := range matches {
		if m.repl.With != nil {
			e.runReplaceWith(e.replCtx(m, ev), ev.Obj, m.repl.With, nil)
		}
	}
	return ev, false
}

// applyTurnFaceUpReplacements is the TurnFaceUp event's replacement pass
// (R:Event$ TurnFaceUp, CR 614.1a with CR 708.6/702.36e, task
// cli-20260924T031747Z-6d0658fc). The bodies resolve BEFORE the turn-up event
// is folded: the counters Hooded Hydra's body places land on the permanent as
// it turns face up, and only then does events.Apply retire the CR 708.5
// face-down set. An augmenting body never prevents the flip -- Forge's
// Replaced reading of these lines: the physical turn-up belongs to the
// turn-up action/effect, the replacement only adds to it -- so after every
// body the event is left intact (handled=false) for the ordinary fold and
// its own trigger matching. A With==nil (Layer$ CantHappen) body -- Karlov
// Watchdog's "permanents your opponents control can't be turned face up",
// Unable to Scream -- IS the complete replacement: the turn-up never happens
// and a Note records the prevention, the same shape the damage-prevention
// arm emits.
func (e *Engine) applyTurnFaceUpReplacements(ev events.Event, matches []replMatch) (events.Event, bool) {
	for _, m := range matches {
		if m.repl.With == nil {
			return e.emit(events.Event{Kind: events.Note, Obj: ev.Obj,
				Text: "turn face up prevented by replacement effect"}), true
		}
	}
	if len(matches) == 1 && strings.EqualFold(matches[0].repl.Params["Optional"], "True") {
		e.replChoices = append(e.replChoices, replChoice{kind: replChoiceFaceUp, ev: ev, cands: matches, selected: 0})
		if e.pending == nil {
			e.askReplacementChoice(e.replacementOptionalDeciderOrController(matches[0]))
		}
		return ev, true
	}
	// Park the transition for the body's whole continuation: a body that asks
	// (Aquamorph Entity's GenericChoice, Gift of Doom's attach) must not let
	// the fold reveal the face before the answer, so resolveReplacementBody --
	// the sole owner of a ReplaceWith$ body's continuation linkage -- appends
	// the re-emit frame to the body's suspension chain. A body that asks
	// nothing leaves the field set and the caller's clear folds the event
	// through the ordinary path.
	e.turnUpMove = &ev
	for _, m := range matches {
		e.runReplaceWith(e.replCtx(m, ev), ev.Obj, m.repl.With, nil)
		if e.pending != nil {
			e.turnUpMove = nil
			return ev, true
		}
	}
	e.turnUpMove = nil
	return ev, false
}

func (e *Engine) replacementOptionalDeciderOrController(m replMatch) state.PlayerID {
	if p, ok := e.replacementOptionalDecider(*m.repl, m.id); ok {
		return p
	}
	return e.controllerOf(m.id)
}

// turnFaceUpCantHappen is the ONE legality predicate for a blocked turn-up:
// it reports whether a live R:Event$ TurnFaceUp CantHappen replacement (a
// printed line with no ReplaceWith$, or an Effect-created bodyless
// registration with Layer$ CantHappen) would prevent id's turn-up outright.
// The special action is then ILLEGAL (CR 614.1a's "can't" stops the action
// before it starts), so the offer (rules/legal.go) and the submitted-option
// guard (the same file's validate switch) both consult this one predicate
// instead of re-implementing the match, and the dispatch's CantHappen arm
// above stays as defence-in-depth for emit routes that never consult the
// offer (an effect-driven SetState turn-up).
func (e *Engine) turnFaceUpCantHappen(id state.ObjID) bool {
	ev := events.Event{Kind: events.TurnFaceUp, Obj: id}
	for _, ce := range e.active() {
		if ce.ReplacementEvent != "TurnFaceUp" || ce.ReplacementBody != "" ||
			!strings.EqualFold(strings.TrimSpace(ce.ReplacementParams["Layer"]), "CantHappen") {
			continue
		}
		r := &cards.Repl{Event: ce.ReplacementEvent, Params: ce.ReplacementParams}
		if e.replacementMatchesEffectCreated(*r, ce.Source, ev, ce.Remembered, ce.RememberedPlayers) {
			return true
		}
	}
	blocked := false
	e.forEachReplacementSource(func(source state.ObjID) {
		if blocked {
			return
		}
		f := e.replacementFace(source, ev)
		if f == nil {
			return
		}
		for i := range f.Repls {
			r := &f.Repls[i]
			if !replacementEventNameMatches(r.Event, "TurnFaceUp") || r.With != nil {
				continue
			}
			if e.replacementMatches(*r, source, ev) {
				blocked = true
				return
			}
		}
	})
	return blocked
}

// phaseStep maps a Forge Phase$ value on a BeginPhase replacement onto the
// engine's step constants. Only the three steps the corpus names are known;
// any other value (or a comma list this build does not split) fails closed,
// leaving the phase to run normally.
func phaseStep(ph string) (state.Step, bool) {
	switch strings.TrimSpace(ph) {
	case "Untap":
		return state.StepUntap, true
	case "Upkeep":
		return state.StepUpkeep, true
	case "Draw":
		return state.StepDraw, true
	}
	return 0, false
}

func (e *Engine) phaseChoice(ev events.Event, candidates []replMatch, used []bool) replChoice {
	rc := replChoice{ev: ev, cands: candidates, applied: append([]bool(nil), used...),
		before: e.retainTriggerBefore()}
	if e.stepLeaving != nil {
		rc.boundary = true
		rc.leaving = *e.stepLeaving
	}
	return rc
}

// posePhaseOrderChoice parks a step entry with all currently applicable
// BeginPhase replacements so the active player chooses which gets the first
// opportunity under CR 616.1.
func (e *Engine) posePhaseOrderChoice(ev events.Event, candidates []replMatch,
	used []bool, applicable []int) {
	rc := e.phaseChoice(ev, candidates, used)
	rc.kind = replChoicePhaseOrder
	rc.applicable = append([]int(nil), applicable...)
	e.replChoices = append(e.replChoices, rc)
	if e.pending == nil {
		e.askReplacementChoice(e.G.Active)
	}
}

// posePhaseOptionalChoice parks the selected Optional$ BeginPhase replacement
// for its apply/decline answer. Declining resumes the remaining candidate set.
func (e *Engine) posePhaseOptionalChoice(ev events.Event, candidates []replMatch,
	used []bool, selected int) {
	rc := e.phaseChoice(ev, candidates, used)
	rc.kind = replChoicePhaseOptional
	rc.selected = selected
	e.replChoices = append(e.replChoices, rc)
	if e.pending == nil {
		e.askReplacementChoice(e.G.Active)
	}
}
