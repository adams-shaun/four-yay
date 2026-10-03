package state

import "strings"

// This file is the ONE parser for Forge script phase names -- the strings a
// card script writes after `Phase$`, `Phases$`, `ActivationPhases$`,
// `ConditionPhases$` and friends. It has Forge PhaseType.smartValueOf /
// PhaseType.parseRange parity (forge-game/src/main/java/forge/game/phase/
// PhaseType.java): a comma-separated list whose elements are either a single
// name (matched case-insensitively against Forge's script names and enum
// names), the special name `Main` (both main phases), the special name `All`
// (every step), or an `A->B` range over the turn order (an open `A->` runs to
// Cleanup). Every phase-name consumer -- Mode$ Phase face triggers
// (rules/trigmatch/actions.go PhaseMatches) and delayed-trigger registrations
// (effects/misc.go effDelayedTrigger) -- must resolve names through
// ParsePhases so the two cannot disagree; the previous two independent
// substring parsers (trigmatch.PhaseMatches's Contains and delayedPhaseStep's switch)
// are the defect class this file replaces.
//
// The remaining narrowing versus Forge's own model: Forge has a separate
// COMBAT_FIRST_STRIKE_DAMAGE step, which gorge's turn does not model (the
// engine has a single combat-damage step). `First Strike Damage` therefore
// maps to StepCombatDamage; rules/trigger_phase_gate.go gates that mapping on a
// first/double striker being present, but cannot distinguish the first-strike
// step from the later regular-damage step. The combat trigger still fires only
// once at the engine's single step boundary.

// StepSet is a bitmask over the twelve steps a turn walks. Zero is the empty
// set, never a step value: the zero Step is StepUntap, so consumers must go
// through Has/Steps/Empty rather than treating a StepSet as a Step.
type StepSet uint16

// Has reports whether step is a member of the set. An invalid step is in no
// set.
func (s StepSet) Has(step Step) bool {
	if !step.Valid() {
		return false
	}
	return s&(1<<uint(step)) != 0
}

// Empty reports whether the set has no member.
func (s StepSet) Empty() bool { return s == 0 }

// Steps returns the set's members in turn order (untap .. cleanup), so a
// caller walking the set (or registering one delayed trigger per member)
// gets a deterministic order straight from the engine's own step numbering,
// never a map iteration.
func (s StepSet) Steps() []Step {
	var out []Step
	for i := 0; i < numSteps; i++ {
		st := Step(i)
		if s.Has(st) {
			out = append(out, st)
		}
	}
	return out
}

// Ordinal returns step's 1-based position within the set in turn order
// (untap .. cleanup), or 0 when step is not a member. This is the N a Forge
// `PhaseCount$ N` gate compares against: `Phase$ Main` names both main
// phases, so the second main phase is ordinal 2 of that set. A repeated step
// (an api:AddPhase-spliced extra upkeep or combat) is counted once -- the
// set's canonical turn order, not the turn's live phase history.
func (s StepSet) Ordinal(step Step) int {
	if !step.Valid() || !s.Has(step) {
		return 0
	}
	n := 0
	for i := 0; i <= int(step); i++ {
		if s.Has(Step(i)) {
			n++
		}
	}
	return n
}

// AllSteps is every step of a turn -- what a `Phase$ All` gate and an absent
// `Phase$` gate both mean.
func AllSteps() StepSet { return (1 << uint(numSteps)) - 1 }

// forgePhaseNames maps the lower-cased Forge script name and the lower-cased
// Forge enum name of each phase to the engine step (or steps, for
// First Strike Damage) it names. Read-only lookups only -- never range over
// it where the order could reach an event.
var forgePhaseNames = func() map[string]StepSet {
	m := make(map[string]StepSet)
	add := func(script, enum string, set StepSet) {
		m[strings.ToLower(script)] |= set
		m[strings.ToLower(enum)] |= set
	}
	add("Untap", "UNTAP", one(StepUntap))
	add("Upkeep", "UPKEEP", one(StepUpkeep))
	add("Draw", "DRAW", one(StepDraw))
	add("Main1", "MAIN1", one(StepMain1))
	add("BeginCombat", "COMBAT_BEGIN", one(StepBeginCombat))
	add("Declare Attackers", "COMBAT_DECLARE_ATTACKERS", one(StepDeclareAttackers))
	add("Declare Blockers", "COMBAT_DECLARE_BLOCKERS", one(StepDeclareBlockers))
	// First Strike Damage has no engine step of its own; see the file doc.
	add("First Strike Damage", "COMBAT_FIRST_STRIKE_DAMAGE", one(StepCombatDamage))
	add("Combat Damage", "COMBAT_DAMAGE", one(StepCombatDamage))
	add("EndCombat", "COMBAT_END", one(StepEndCombat))
	add("Main2", "MAIN2", one(StepMain2))
	add("End of Turn", "END_OF_TURN", one(StepEnd))
	add("Cleanup", "CLEANUP", one(StepCleanup))
	return m
}()

func one(s Step) StepSet { return 1 << uint(s) }

// ParsePhases parses one Forge phase-name expression into the engine steps
// it names, returning the set and, in the order the spec wrote them, the
// elements it could not resolve. An element that resolves to nothing is
// reported, never silently matched or dropped (Forge's smartValueOf throws
// on an unknown name; this build degrades to a reported no-op instead). An
// empty spec resolves to the empty set with nothing unknown -- a caller
// decides for itself what an absent gate means (a Mode$ Phase trigger with
// no Phase$ param is ungated; a delayed trigger with one is an error).
//
// Range elements use Forge's turn order: `Untap->Draw` is Untap, Upkeep,
// Draw; an open `Upkeep->` runs to Cleanup; a range whose either end is
// unknown reports the whole element. `Main` names both main phases and
// `All` names every step. Unlike Forge -- whose `Main` and `All` checks are
// exact-case -- the matches here are case-insensitive, in line with the
// smartValueOf name comparison, so `main`, `end of turn` and `END_OF_TURN`
// all resolve; the corpus writes `End Of Turn` (24 lines) and relies on
// exactly this tolerance.
func ParsePhases(spec string) (StepSet, []string) {
	if strings.TrimSpace(spec) == "" {
		return 0, nil
	}
	var set StepSet
	var unknown []string
	for el := range strings.SplitSeq(spec, ",") {
		el = strings.TrimSpace(el)
		if el == "" {
			// Forge: smartValueOf("") throws. Report the empty element
			// rather than matching or dropping it silently.
			unknown = append(unknown, el)
			continue
		}
		if idx := strings.Index(el, "->"); idx >= 0 {
			fromName := strings.TrimSpace(el[:idx])
			toName := strings.TrimSpace(el[idx+2:])
			from, fromOK := resolveOnePhase(fromName)
			if !fromOK {
				unknown = append(unknown, el)
				continue
			}
			to := StepCleanup
			if toName != "" {
				tStep, toOK := resolveOnePhase(toName)
				if !toOK {
					unknown = append(unknown, el)
					continue
				}
				to = tStep
			}
			// Forge's EnumSet.range rejects a backwards closed range rather
			// than producing an empty set. Report the complete element here
			// too: silently accepting it would make a misspelled/reversed
			// Phase$ gate indistinguishable from a deliberately empty one.
			if from > to {
				unknown = append(unknown, el)
				continue
			}
			for i := int(from); i <= int(to) && i < numSteps; i++ {
				set |= one(Step(i))
			}
			continue
		}
		switch strings.ToLower(el) {
		case "main":
			set |= one(StepMain1) | one(StepMain2)
		case "all":
			set |= AllSteps()
		default:
			resolved, ok := resolvePhaseName(el)
			if !ok {
				unknown = append(unknown, el)
				continue
			}
			set |= resolved
		}
	}
	return set, unknown
}

// resolvePhaseName resolves a single Forge phase name (script name or enum
// name, case-insensitively) to its step set; ok=false for anything else.
func resolvePhaseName(name string) (StepSet, bool) {
	set, ok := forgePhaseNames[strings.ToLower(strings.TrimSpace(name))]
	return set, ok
}

// resolveOnePhase resolves a single Forge phase name (script name or enum
// name, case-insensitively) to exactly one step; ok=false for anything else,
// including the multi-step names Main and All (a range end must be a
// singleton, as Forge's EnumSet.range requires).
func resolveOnePhase(name string) (Step, bool) {
	set, ok := resolvePhaseName(name)
	if !ok {
		return 0, false
	}
	steps := set.Steps()
	if len(steps) != 1 {
		return 0, false
	}
	return steps[0], true
}

// ExtraPhaseRangeEnd returns the LAST step of the extra phase that begins at
// entry -- the step whose leaving completes the extra phase (rules/turn.go's
// consumer). Forge's ExtraPhase$ values: Combat is the whole combat
// (BeginCombat..EndCombat), Beginning the whole beginning phase
// (Untap..Upkeep..Draw, whose Turn-Based-Actions all run -- Shadow of the
// Second Sun's added beginning phase untaps and DRAWS), and the single-step
// values Upkeep and End of Turn are their own range. An unknown entry is its
// own range (a single-step extra phase).
func ExtraPhaseRangeEnd(entry Step) Step {
	switch entry {
	case StepBeginCombat:
		return StepEndCombat
	case StepUntap:
		return StepDraw
	default:
		return entry
	}
}

// EarliestAfter returns the set member that comes first in turn order
// strictly after cur, wrapping once past cleanup -- the step a one-shot
// "at the beginning of the next ..." delayed trigger registers for. A
// single-step set always returns that same step a delayedPhaseStep-style
// parser would (the wrap only matters when the registered step has already
// passed this turn, where the next occurrence is next turn's), and a
// multi-step set collapses to the FIRST of its members the game will still
// reach -- which is what Forge's one-fire delayed trigger (removed from
// TriggerHandler.delayedTriggers the moment it fires) does. ok=false only
// for an empty set.
func EarliestAfter(set StepSet, cur Step) (Step, bool) {
	if set.Empty() {
		return 0, false
	}
	start := 0
	if cur.Valid() {
		start = int(cur) + 1
	}
	for wrap := 0; wrap < 2; wrap++ {
		for i := start; i < numSteps; i++ {
			if set.Has(Step(i)) {
				return Step(i), true
			}
		}
		start = 0 // second pass wraps past cleanup into the next turn.
	}
	return 0, false
}
