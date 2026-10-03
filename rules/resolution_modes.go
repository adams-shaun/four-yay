// resolution_modes.go holds the mid-resolution modal answers: handleModes, the as-enters entry choice (resumeETBEntry) and the unless-pay option lookup.

// Code moved verbatim out of rules/resolution.go (moving code only; the
// suspension/resumption mechanism is documented at the top of resolution.go).
package rules

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// handleModes applies an answered KModes decision. ResumeKind and the trigger
// drain flag distinguish three lifetimes: a modal spell's CR 601.2b cast
// proposal, a modal trigger's CR 603.3c placement, and an effect suspended in
// mid-resolution (including unless-pay). Every branch records ModeChosen; the
// first two also cache the chosen SVar names on the stack object so resolution
// executes the announcement without asking again.
func (e *Engine) handleModes(d *decision.Decision, in decision.Intent) {
	if ma, rp := e.takeOffStackManaRider(); ma != nil {
		e.resume = rp
		template := *ma
		template.nestedResume = nil
		asked := e.withOffStackMana(template, func() { e.handleModes(d, in) })
		e.finishOffStackManaRider(ma, asked)
		return
	}
	// An activated mana ability resolves outside the stack. Its UnlessCost$
	// answer is therefore owned by the mana activation flow rather than an
	// effects resume point, but is still recorded like every KModes answer.
	if d.ResumeKind == "villainous" {
		if e.resume == nil {
			e.emit(events.Event{Kind: events.Note, Player: in.Player,
				Text: "villainous choice answered with no resolution suspended"})
			return
		}
		rp := e.resume
		e.resume = nil
		chosen := d.Chosen(in)
		if len(chosen) > 0 && chosen[0].Index >= 0 && chosen[0].Index < len(d.ResumeModes) {
			rp.villainousChoice = d.ResumeModes[chosen[0].Index]
		}
		e.emit(events.Event{Kind: events.ModeChosen, Obj: rp.obj, Player: in.Player,
			Text: strings.Join(chosenModeLabels(chosen), ",")})
		e.resumeResolution(rp, chosen)
		return
	}
	// A multi-player api:GenericChoice's per-chooser KModes answer. Like the
	// villainous arm, the answer is scoped to one chooser: record the chosen
	// SVar name and resume the GenericChoice SA, which runs that chooser's body
	// and then asks the next Defined$ chooser. The cursor itself rides the
	// resume point (ResumeGenericChoosers/Index), so it is not re-derived here.
	if d.ResumeKind == "generic_players" {
		if e.resume == nil {
			e.emit(events.Event{Kind: events.Note, Player: in.Player,
				Text: "generic choice answered with no resolution suspended"})
			return
		}
		rp := e.resume
		e.resume = nil
		chosen := d.Chosen(in)
		if len(chosen) > 0 && chosen[0].Index >= 0 && chosen[0].Index < len(d.ResumeModes) {
			rp.genericChoice = d.ResumeModes[chosen[0].Index]
		}
		e.emit(events.Event{Kind: events.ModeChosen, Obj: rp.obj, Player: in.Player,
			Text: strings.Join(chosenModeLabels(chosen), ",")})
		e.resumeResolution(rp, chosen)
		return
	}
	if d.ResumeKind == "mana_unless" {
		chosen := d.Chosen(in)
		labels := chosenModeLabels(chosen)
		e.emit(events.Event{Kind: events.ModeChosen, Obj: d.Source, Player: in.Player,
			Text: strings.Join(labels, ",")})
		e.choosing = chooseNone
		e.answerManaUnless(chosen)
		return
	}

	// CR 601.2b cast branch: the spell is already provisionally on the stack,
	// but no targets have been selected and no cost has been paid. Record the
	// answer on that spell, then resume the cast transaction at target choice.
	// CR 702.55 dredge: a draw-step draw replaced by a graveyard dredge is
	// NOT a stack object, so the ordinary mid-resolution resume path (which
	// re-enters a suspended stack-object resolution) does not apply. Handle
	// the answered dredge here: option 0 mills the dredge card's N and returns
	// it to hand (the ordinary draw is already skipped by the ask's
	// suspension), option 1 (or an empty answer) lets the draw happen, which
	// the suspended DrawFor re-runs as the ordinary draw.
	if d.ResumeKind == "dredge" && (e.resume == nil || e.resume.direct) {
		// A turn-based draw has no enclosing stack resolution to re-enter.
		// A Draw API on a resolving spell/ability instead falls through to
		// resumeResolution below, which restores its cursor and finishes every
		// remaining draw and SubAbility$ exactly once.
		ch := d.Chosen(in)
		if len(ch) > 0 && ch[0].Kind == "dredge" {
			e.applyDredge(in.Player, ch[0].Obj)
		} else {
			e.resumeOrdinaryDraw(in.Player)
		}
		// A GainLife→Draw replacement body parked its remaining draws on this
		// ask (replacement.go's lifeReplacementDraw): the answer resolved the
		// draw that asked, so re-drive the rest -- which may park again on
		// the next dredge ask -- and then drain any replacement-order queue
		// the interrupted pass left behind.
		if rp := e.resume; rp != nil && rp.lifeDraws > 0 {
			rest := rp.lifeDraws
			e.resume = nil
			e.lifeReplacementDraw(in.Player, rest)
			e.askNextReplacementChoice()
			return
		}
		e.resume = nil
		return
	}
	if d.ResumeKind == "cast_modes" {
		e.applyCastModes(d, in.Player, d.Chosen(in))
		return
	}

	// CR 603.3c placement branch: this KModes decision was asked by the
	// trigger drain (pushTrigger's askTriggerModes) rather than posed
	// mid-resolution by an effect. There is no suspension to resume -- the
	// answer is recorded onto the trigger's stack object (ChosenModes, a
	// cache of the logged answer) so resolveTop builds Ctx.Modes from it and
	// effCharm runs exactly the chosen modes instead of asking again -- and
	// the drain resumes through the same continuation every other trigger
	// drain answer uses. drainAwaitsModes identifies the branch; it is false
	// for a mid-resolution ask, which falls through to the resume path below.
	if e.drainAwaitsModes {
		e.drainAwaitsModes = false
		chosen := d.Chosen(in)
		labels := chosenModeLabels(chosen)
		names := modeChoiceNames(d.ResumeSA, chosen, d.ResumeModes)
		if len(e.G.Stack) > 0 {
			id := e.G.Stack[len(e.G.Stack)-1]
			var so *state.Object
			if o := e.G.Obj(id); o != nil {
				so = o
				// modeChoiceNames is non-nil even for zero chosen modes,
				// so a zero-mode placement resolves as nothing.
				o.ChosenModes = names
			}
			// ChoiceRestriction$: record the placement pick on the trigger's
			// SOURCE (the permanent), not on the transient stack object, so the
			// next trigger instance of the same Charm sees it -- including when
			// a second instance is already waiting in the queue. A no-op unless
			// the SA carries the param.
			if so != nil {
				effects.RecordCharmChoices(e, so.Source, d.ResumeSA, names)
			}
			e.emit(events.Event{Kind: events.ModeChosen, Obj: id, Player: in.Player,
				Text: strings.Join(labels, ",")})
			// A trigger Charm's targeting lives INSIDE its modes (the
			// corpus pairs ValidTgts$ on the chosen mode's SVar body,
			// never on the ability — Charming Scoundrel's DBToken), so the
			// modes the player just chose ask their targets now, at the same
			// placement moment CR 603.3c puts the mode choice. The cross-mode
			// TargetUnique family (Shadrix Silverquill, the duo cycle, Balor,
			// Vindictive Lich, Chaos Balor) asks ONE combined KTarget over the
			// shared player pool with per-player Option.Group exclusivity —
			// Decision.Validate's mutual-exclusion rule IS the "each mode must
			// target a different player" rule, enforced on the wire — and the
			// answers attribute to the target-bearing modes positionally in
			// chosen-mode order (ask order == chosen order == replay order).
			// Distinct single-target modes use their own grouped options and
			// target bindings. Other declarations keep their legacy ask path.
			// The answer lands on the stack object through handleTarget's
			// ordinary record; effToken (TokenOwner$ ThisTargetedPlayer) and
			// friends read the mode's own c.Targets.
			if so != nil && so.Ability != nil {
				if src := e.G.Obj(so.Source); src != nil && src.Face() != nil {
					svars := src.Face().SVars
					var tbms []*cards.SA
					for _, name := range names {
						if sub := cards.ResolveSVar(svars, name); sub != nil &&
							strings.TrimSpace(sub.ParamStr(cards.PKValidTgts)) != "" {
							tbms = append(tbms, sub)
						}
					}
					choices := effects.CharmOf(so.Ability).Modes
					status, why := effects.CharmCrossModeShape(svars, choices)
					if status == effects.CharmUniqueSupported && len(tbms) >= 2 {
						e.drainAwaitsTarget = true
						if e.askCrossModeCharmTargets(in.Player, id, tbms) {
							return
						}
						// Fewer legal candidates than target-bearing modes: the
						// combined different-player ask is unsatisfiable. Fall
						// through to the historical first-mode ask below, whose
						// own bounds/insufficiency handling governs.
					} else if status == effects.CharmUniqueUnsupported {
						e.emit(events.Event{Kind: events.Note, Obj: id,
							Text: "cross-mode TargetUnique$ Charm shape unimplemented: " + why})
					} else if len(tbms) >= 2 {
						asked, infeasible := e.askCharmModeTargets(in.Player, id, svars, so.Ability, names)
						if infeasible {
							// CR 603.3c: a triggered ability whose announced modes
							// cannot all acquire mandatory targets cannot resolve.
							// Remove it instead of posing an unanswerable decision
							// or silently targeting only the first mode.
							e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZStack,
								To: state.ZExile, Text: "countered: no legal modal targets"})
							e.ensureLeftTheStack(id, state.ZExile, "a replacement discarded an untargetable modal ability's move")
							e.drainAwaitsTarget = false
							e.resumeTriggerDrain()
							return
						}
						if asked {
							e.drainAwaitsTarget = true
							return
						}
					}
					for _, sub := range tbms {
						e.drainAwaitsTarget = true
						e.askTarget(in.Player, id, sub)
						return
					}
				}
			}
		}
		e.resumeTriggerDrain()
		return
	}
	if e.resume == nil {
		e.emit(events.Event{Kind: events.Note, Player: in.Player,
			Text: "modes answered with no resolution suspended"})
		return
	}
	rp := e.resume
	e.resume = nil
	chosen := d.Chosen(in)
	labels := chosenModeLabels(chosen)
	if d.ResumeSA != nil && strings.EqualFold(d.ResumeSA.Params["SetChosenMode"], "True") && len(chosen) == 1 {
		// An as-enters GenericChoice records its mode on the permanent via
		// the event fold; the ModeChosen marker alone stores no object state.
		if names := modeChoiceNames(d.ResumeSA, chosen, d.ResumeModes); len(names) == 1 {
			e.emit(events.Event{Kind: events.Choose, Obj: d.Source, Counter: "mode", Text: names[0]})
		}
	}
	e.emit(events.Event{Kind: events.ModeChosen, Obj: rp.obj, Player: in.Player,
		Text: strings.Join(labels, ",")})
	// ChoiceRestriction$: a mid-resolution Charm's pick is recorded on its
	// source as well, so a later instance is restricted against it.
	if o := e.G.Obj(rp.obj); o != nil {
		effects.RecordCharmChoices(e, o.Source, d.ResumeSA,
			modeChoiceNames(d.ResumeSA, chosen, d.ResumeModes))
	}
	e.resumeResolution(rp, chosen)
}

// resumeETBEntry is the resolution-owned continuation for an as-enters
// choice. Keeping the e.resume write here preserves the structural invariant
// that only resolution machinery consumes a suspended frame.
func (e *Engine) resumeETBEntry(chosen []decision.Option) state.ObjID {
	// handleChoose owns clearing e.resume; this continuation only consumes the
	// parked entry, keeping the archtest's single ownership rule intact.
	if e.etbMove == nil || len(chosen) != 1 {
		e.etbMove = nil
		e.etbNext = 0
		e.choosing = chooseNone
		return 0
	}
	move := *e.etbMove
	opt := chosen[0]
	switch opt.Kind {
	case "name":
		e.emit(events.Event{Kind: events.Choose, Obj: move.Obj, Counter: "name", Text: opt.Label})
	case "type":
		e.emit(events.Event{Kind: events.Choose, Obj: move.Obj, Counter: "type", Text: opt.Label})
	case "number":
		e.emit(events.Event{Kind: events.Choose, Obj: move.Obj, Counter: "number", Amount: int32(opt.Amount)})
	case "color":
		if letter := etbColourLetter(opt.Label); letter != "" {
			e.emit(events.Event{Kind: events.Choose, Obj: move.Obj, Counter: "color", Text: letter})
		}
	case "evenodd":
		quality := strings.ToLower(opt.Label)
		if quality == "odd" || quality == "even" {
			e.emit(events.Event{Kind: events.Choose, Obj: move.Obj, Counter: "type", Text: quality})
		}
	case "riot":
		choice := "haste"
		if opt.Index == 0 {
			choice = "counter"
		}
		e.emit(events.Event{Kind: events.Choose, Obj: move.Obj, Counter: "riot", Text: choice})
	case "unleash":
		choice := "plain"
		if opt.Index == 0 {
			choice = "counter"
		}
		e.emit(events.Event{Kind: events.Choose, Obj: move.Obj, Counter: "unleash", Text: choice})
	case "clone":
		// The ETB-copy election (K:ETBReplacement:Copy). The chosen template
		// rides the event's IDs; the decline ("Enter as itself") carries no
		// object, so the fold records an answered-but-empty choice and the
		// ETBReplacement body -- effects' effClone, reached at the re-emitted
		// move below -- leaves the object entering as itself.
		ids := []state.ObjID(nil)
		if opt.Obj != 0 {
			ids = []state.ObjID{opt.Obj}
		}
		e.emit(events.Event{Kind: events.Choose, Obj: move.Obj, Counter: "clone", IDs: ids})
	case "paylife":
		// The announced life payment of an "as CARDNAME enters, pay any amount
		// of life" replacement (Minion of the Wastes / Phyrexian Processor /
		// Nameless Race). The announced X is recorded as a Choose "number"
		// entry (the same fold a ChooseNumber uses), bound onto the object as
		// its paid X (events.XChange, so replCtx's `X: o.X` hands it to the
		// replacement body's Count$xPaid), and paid as one LifeChange before
		// the move is re-emitted -- the body then stores the paid amount
		// through events.StoreSVar. CR 118.3 (paying life), CR 601.2b
		// (announcing X).
		x := int32(opt.Amount)
		if x < 0 {
			x = 0
		}
		e.emit(events.Event{Kind: events.Choose, Obj: move.Obj, Counter: "number", Amount: x})
		e.emit(events.Event{Kind: events.XChange, Obj: move.Obj, Amount: x})
		if x > 0 {
			e.emit(events.Event{Kind: events.LifeChange, Player: opt.Player, Amount: -x})
		}
	}
	e.choosing = chooseNone
	e.emit(move)
	// The answered entry has been re-emitted: if it was a land play and the
	// entry was fully replaced (or replaced again after another as-enters
	// answer), settle the land play here rather than leaving the continuation
	// armed for an unrelated later entry to consume.
	e.settleLandPlayIfDone(move.Obj)
	return move.Obj
}

// continueAfterETBEntry hands an as-enters entry choice's answer back to the
// resolution that entry interrupted. Engine.Ask parks the resolving object on
// every mid-resolution ask, and applyETBChoiceReplacement's ask is posed from
// inside emit, so the frame it parks is whatever effect was moving the object
// onto the battlefield (a reanimation, a blink, Retether's mass Aura return).
// resumeETBEntry has already completed the entry itself, so the frame resumes
// with no answer: its recorded continuation (rp.outer) runs and the stack
// object is finished, instead of being left on the stack for resolveTop to
// resolve a second time.
//
// Direct frames have no interrupted stack resolution, and frames whose object
// has left the stack independently have no remaining resolution to finish. A
// permanent spell's own entry is different: its parked move is re-emitted by
// the answer, so resolveTop never reached its completion tail. Finish that
// spell here, from the same resolution-owned continuation used by other
// suspended resolutions.
func (e *Engine) continueAfterETBEntry(rp *resumePoint, entry state.ObjID) {
	if rp == nil || rp.direct || rp.obj == 0 || e.pending != nil {
		return
	}
	if rp.obj == entry {
		e.finishResumption(rp.obj)
		e.emit(events.Event{Kind: events.Priority, Player: e.G.Active})
		return
	}
	if o := e.G.Obj(rp.obj); o == nil || o.Zone != state.ZStack {
		return
	}
	e.resumeResolution(rp, nil)
}

// resumeResolution re-enters a suspended resolution with its answer. It
// rebuilds the same Ctx resolveTop built for the object on its first pass
// (Source/Controller/Targets/Remembered and the SVar table are all
// re-derivable from the stack object, which has not moved), attaches the
// answer, and re-runs the suspended sub-ability — effects.Resolve walks
// from it through the rest of the chain, which is precisely the
// continuation that had not run yet. If that continuation asks again the
// new pending point is linked after this one's own continuation and the
func unlessPayChoice(chosen []decision.Option) (decision.Option, bool) {
	for _, option := range chosen {
		if option.Mode == decision.ModeUnlessPay {
			return option, true
		}
	}
	return decision.Option{}, false
}
