package rules

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// openTargetBatch/closeTargetBatch bracket ONE targeting action's TargetsChosen
// events for the Mode$ BecomesTargetOnce latch. recordChosenTargets
// (rules/stack.go) opens the bracket, emits one event per chosen target, and
// closes it, so every target of one target answer is one batch. It is NOT the
// only emitter of TargetsChosen: effects/choose_control.go's recordTargets (a
// ChangeTargets redirect, CR 114.6) also emits, with NO bracket open, so each
// such event is its own batch-of-one and a multi-target redirect fires a
// BecomesTargetOnce watcher once per redirected target rather than once per
// redirect action (Forge fires once per ability). No corpus carrier exercises
// that path today (Psychic Battle, the only ValidCause$ card, excludes itself;
// Leyline/Hojo never watch a redirect), so the divergence is latent. The latch
// map is per-batch scratch (the damage/zone/mill/discard batches' shape) and
// never survives the close. A hand-built emit outside any bracket is its own
// batch-of-one.
func (e *Engine) openTargetBatch() {
	e.targetBatchOpen = true
	e.targetBatchFired = nil
}

func (e *Engine) closeTargetBatch() {
	e.targetBatchOpen = false
	e.targetBatchFired = nil
}

type parsedPhase struct {
	set   state.StepSet
	valid bool
}

// parsePhaseSpec is parsedPhaseSpec's pure parse.
func parsePhaseSpec(spec string) parsedPhase {
	set, unknown := state.ParsePhases(spec)
	return parsedPhase{set: set, valid: len(unknown) == 0}
}

// parsedPhaseSpec caches syntax only, never whether the current step matches.
// Diagnostic scans and live/look-back matchers use the same parse semantics;
// only the live scan emits Notes, tracked separately in phaseUnknownNoted.
func (e *Engine) parsedPhaseSpec(spec string) parsedPhase {
	if p, ok := e.phaseSpecs[spec]; ok {
		return p
	}
	p := parsePhaseSpec(spec)
	if e.phaseSpecs == nil {
		e.phaseSpecs = make(map[string]parsedPhase)
	}
	e.phaseSpecs[spec] = p
	return p
}

// phaseGate applies Forge's Phase$ (validPhases) uniformly to every trigger
// mode. It is deliberately before the mode switch in triggerMatches: a
// ChangesZone or SpellCast trigger with Phase$ Main1 must not fire during an
// upkeep, and an unresolvable name fails closed. checkFaceTriggers reports
// that invalid name once as a Note; this bool-only matcher does not emit
// while it may be walking a scratch look-back observer. An absent Phase$
// remains ungated, matching Forge's null validPhases.
//
// PhaseCount$ narrows a Phase$ set to the Nth member of that set in turn
// order: `Phase$ Main | PhaseCount$ 2` is the SECOND main phase, so the gate
// fails at the first. A non-positive or non-numeric value fails closed (the
// conservative direction -- the trigger then fires at no step rather than
// every matching one).
func (e *Engine) phaseGate(t cards.Trigger) bool {
	spec := t.ParamStr(cards.PKPhase)
	if strings.TrimSpace(spec) == "" {
		return true
	}
	p := e.parsedPhaseSpec(spec)
	if !p.valid || !p.set.Has(e.G.Step) {
		return false
	}
	// gorge has one combat-damage step, while Forge distinguishes the
	// first-strike damage step. Until the turn walk has that separate step,
	// only let this mapping match when a first/double striker is actually in
	// combat; otherwise the named phase does not occur at all.
	for phase := range strings.SplitSeq(spec, ",") {
		phase = strings.TrimSpace(phase)
		if strings.EqualFold(phase, "First Strike Damage") ||
			strings.EqualFold(phase, "COMBAT_FIRST_STRIKE_DAMAGE") {
			if e.G.Step != state.StepCombatDamage || !e.anyFirstStrike() {
				return false
			}
		}
	}
	count := strings.TrimSpace(t.Params["PhaseCount"])
	if count == "" {
		return true
	}
	n, err := strconv.Atoi(count)
	if err != nil || n < 1 {
		return false
	}
	return p.set.Ordinal(e.G.Step) == n
}
