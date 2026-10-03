package effects

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func init() {
	Register("AddPhase", effAddPhase)
}

// delayedTriggerSpec resolves a DelayedTrigger DEFINITION SVar (the
// ExtraTurnDelayedTrigger$/ExtraPhaseDelayedTrigger$ value) to its Phase$
// step and ValidPlayer$ value. Two corpus shapes: the raw
// "Mode$ Phase | Phase$ ..." body (Final Fortune, Moraug -- parseSA cannot
// read it, a definition body has no DB$ head, so cards.ResolveSVar misses
// it and cards.ParseTriggerLine is the reader that shape needs) and the
// keyword expansions' "DB$ DelayedTrigger | Mode$ Phase | ..." body
// (ResolveSVar reads it). ok=false leaves the caller's fallback -- the
// registration the consumer side makes without a phase, never a wrong one.

func delayedTriggerSpec(c *Ctx, name string) (state.Step, string, bool) {
	if c == nil || c.SVars == nil || name == "" {
		return 0, "", false
	}
	if sa := cards.ResolveSVar(c.SVars, name); sa != nil {
		return parseDelayedTriggerBody(sa.ParamStr(cards.PKPhase), sa.ParamStr(cards.PKValidPlayer))
	}
	raw, found := c.SVars[name]
	if !found {
		return 0, "", false
	}
	if trig, ok := cards.ParseTriggerLine(raw); ok {
		return parseDelayedTriggerBody(trig.ParamStr(cards.PKPhase), trig.ParamStr(cards.PKValidPlayer))
	}
	return 0, "", false
}

// parseDelayedTriggerBody resolves a DelayedTrigger body's Phase$ through
// the ONE shared phase-name parser; ok=false on anything unparseable.
func parseDelayedTriggerBody(phase, vp string) (state.Step, string, bool) {
	set, unknown := state.ParsePhases(phase)
	if len(unknown) > 0 || set.Empty() {
		return 0, "", false
	}
	return set.Steps()[0], strings.TrimSpace(vp), true
}

// effAddPhase implements DB$ AddPhase (Forge's AddPhaseEffect; 56 corpus SA
// lines across 56 files: "after this phase, there is an additional combat
// phase" -- Aurelia the Warleader, Aggravated Assault, Moraug, Fury of
// Akoum, Éomer, Marshal of Rohan, Obeka, Splitter of Seconds, ...). The
// grant is one events.ExtraPhase (+1) whose fold is state.Game.ExtraPhases
// (state/game.go); the turn structure (rules/turn.go's advanceStep tail)
// is the consumer, the ExtraTurn/ExtraTurnQueue precedent one level up:
//
//   - ExtraPhase$ names the extra phase: Combat (48 corpus lines) is the
//     whole combat (BeginCombat..EndCombat), Beginning (4) the whole
//     beginning phase (Untap..Draw -- the added untap, upkeep AND draw all
//     run, Shadow of the Second Sun's oracle), Upkeep (3) and End of Turn
//     (1) single steps, and any other multi-step value -- a comma list or
//     A->B range ("Upkeep,Draw") -- resolves to the whole named phase
//     (entry = its first step, range = its last, the RANGEEND rider).
//     A non-contiguous multi-step set -- or an unresolvable value -- is a
//     loud Note and no grant (Forge's smartValueOf throws; the no-grant
//     read is this build's equivalent, never a wrong grant).
//   - AfterPhase$ names the splice point (EndCombat 34, "End of Turn" 1 --
//     a singleton, or a multi-step set naming a whole phase, spliced after
//     its LAST step); when omitted the grant splices after the phase it
//     resolves in, so the granting step rides the event (Step) and replay
//     folds the same splice the live game makes. An unparseable value is a
//     loud Note and the omitted form (the current step) stands in.
//   - FollowedBy$ (Main2, 12 lines -- the Aggravated Assault "followed by an
//     additional main phase" family) names the resume point after the extra
//     phase completes -- a singleton, or a multi-step set resumed at its
//     FIRST step (the walk enters the followed phase). Default (Forge's):
//     the phase that would naturally have followed the splice point
//     (AfterStep+1) -- which is what makes an extra Beginning spliced after
//     Main2 resume at the END STEP, never back into Main1. An unparseable
//     value is a loud Note and the default stands in.
//   - NumPhases$ (Obeka's "you get that many additional upkeep steps",
//     TriggerCount$DamageAmount) is the grant count through the ordinary Num
//     SVar indirection; an unresolvable value is a loud Note and grants one.
//   - SubAbility$ chains through the ordinary effect walk (free).
//   - ConditionPhases$ / ConditionPlayerTurn$ / ConditionFirstCombat$ and
//     the Present/Defined/Compare/SVar family are the shared condition gate
//     (effects/conditions.go), run by the dispatch before this effect.
//
// The ExtraPhaseDelayedTrigger$/ExtraPhaseDelayedTriggerExcute$ pair
// (Moraug's "At the beginning of that combat, untap all creatures you
// control"; note the corpus's "Excute" typo -- read verbatim) forwards the
// delayed trigger on the grant event the way effAddTurn forwards Final
// Fortune's: the DelTrig SVar's Phase$ and ValidPlayer$ are parsed through
// delayedTriggerSpec (the ONE shared definition-body parser) and ride the
// grant Text's ExtraPhaseRiders marker, the Execute$ name rides Counter;
// events.Apply registers the one-shot delayed trigger at CONSUME time with
// MinTurn = the current turn, so it fires exactly once, at the extra
// phase's entry step. An unresolvable DelTrig SVar or an unparseable Phase$
// is a loud Note and the grant still applies without the rider.
func effAddPhase(h Host, c *Ctx, sa *cards.SA) {
	entry, rangeEnd, note := parseExtraPhaseValue(sa.Params["ExtraPhase"])
	if note != "" {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
			Text: note})
		return
	}
	n := int32(1)
	if raw, present := sa.Params["NumPhases"]; present && strings.TrimSpace(raw) != "" {
		if v, resolved := NumResolved(h, c, sa, "NumPhases", 1); resolved {
			n = v
		} else {
			// The documented loud-degrade convention: one Note naming the
			// param, the grant still applied (as one).
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
				Text: "unresolvable NumPhases$ " + strings.TrimSpace(raw) + " (granting one)"})
		}
	}
	if n <= 0 {
		return
	}

	g := h.Game()
	after := g.Step
	if raw := strings.TrimSpace(sa.Params["AfterPhase"]); raw != "" {
		if set, unknown := state.ParsePhases(raw); len(unknown) == 0 && !set.Empty() {
			// The splice point: a named phase's END -- its LAST step (a
			// multi-step set like Beginning or a range splices after the
			// whole phase has run; a singleton is its own end).
			steps := set.Steps()
			after = steps[len(steps)-1]
		} else {
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
				Text: "unparseable AfterPhase$ " + raw + " (splicing after the current phase)"})
		}
	}

	ids := []state.ObjID{state.ObjID(entry)}
	if raw := strings.TrimSpace(sa.Params["FollowedBy"]); raw != "" {
		if set, unknown := state.ParsePhases(raw); len(unknown) == 0 && !set.Empty() {
			// The resume point: a named phase's BEGINNING -- its FIRST step
			// (the walk enters the followed phase, never a step into it).
			steps := set.Steps()
			ids = append(ids, state.ObjID(steps[0]))
		} else {
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
				Text: "unparseable FollowedBy$ " + raw + " (resuming after the splice point)"})
		}
	}

	// The rider payload: a multi-step ExtraPhase$ whose range is not the
	// entry's default carries its last step (the fold would otherwise derive
	// the entry's own range), beside any forwarded delayed trigger.
	var riders events.ExtraPhaseRiders
	if rangeEnd != state.ExtraPhaseRangeEnd(entry) {
		riders.HasRangeEnd, riders.RangeEnd = true, rangeEnd
	}
	text := events.EncodeExtraPhaseRiders(riders)
	counter := ""
	if name := strings.TrimSpace(sa.Params["ExtraPhaseDelayedTrigger"]); name != "" {
		if phase, vp, ok := delayedTriggerSpec(c, name); ok {
			// The Text marker carries the forwarded delayed phase (the
			// consume-time registration reads it); the Execute$ name rides
			// Counter and a ValidPlayer$ rides the marker's VP= part.
			riders.HasDelayedPhase, riders.DelayedPhase, riders.ValidPlayer = true, phase, vp
			text = events.EncodeExtraPhaseRiders(riders)
		} else {
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
				Text: "unparseable ExtraPhaseDelayedTrigger$ " + name + " (no delayed trigger)"})
		}
	}
	// The corpus writes the Execute name under the misspelled parameter
	// ("Excute"); read it verbatim, like Forge's own AddPhaseEffect does.
	if name := strings.TrimSpace(sa.Params["ExtraPhaseDelayedTriggerExcute"]); name != "" {
		counter = name
	}

	h.Emit(events.Event{Kind: events.ExtraPhase, Player: c.Controller, Obj: c.Source,
		Amount: n, Step: after, Counter: counter, Text: text, IDs: ids})
}

// parseExtraPhaseValue resolves a Forge ExtraPhase$ value to the extra
// phase's entry step and its range end -- the LAST step of the named
// phase(s), whose leaving completes the extra phase (rules/turn.go's
// consumer). A multi-step value resolves to entry = its FIRST step and
// rangeEnd = its LAST (the contiguous walk Entry..RangeEnd the fold's queue
// stores): the compound names Combat (BeginCombat..EndCombat) and Beginning
// (Untap..Draw) are not Forge phase NAMES (state.ParsePhases does not know
// bare "Combat"), so they map here, and every other value -- a singleton or
// a multi-step set/range -- goes through the ONE shared phase-name parser.
// A non-contiguous multi-step set ("Untap,Main2") has no contiguous walk to
// store, so it fails closed: note names the value, no grant. An
// unresolvable value fails closed the same way (Forge's smartValueOf
// throws; this is the loud-degrade equivalent), never a wrong grant.
func parseExtraPhaseValue(v string) (state.Step, state.Step, string) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "combat":
		return state.StepBeginCombat, state.StepEndCombat, ""
	case "beginning":
		return state.StepUntap, state.StepDraw, ""
	}
	set, unknown := state.ParsePhases(v)
	if len(unknown) > 0 || set.Empty() {
		return 0, 0, "unimplemented ExtraPhase$ value " + v
	}
	steps := set.Steps()
	for i, s := range steps {
		if int(s)-int(steps[0]) != i {
			return 0, 0, "non-contiguous ExtraPhase$ value " + v
		}
	}
	if len(steps) == 1 {
		return steps[0], state.ExtraPhaseRangeEnd(steps[0]), ""
	}
	return steps[0], steps[len(steps)-1], ""
}
