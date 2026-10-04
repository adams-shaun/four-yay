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
	if d.ResumeKind == "mana_unless" {
		chosen := d.Chosen(in)
		labels := chosenModeLabels(chosen)
		e.emit(events.Event{Kind: events.ModeChosen, Obj: d.Source, Player: in.Player,
			Text: strings.Join(labels, ",")})
		e.choosing = chooseNone
		e.answerManaUnless(chosen)
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
	// for a mid-resolution ask, which the kernel answers from its tape.
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
							effects.TargetsOf(sub).Targeted() {
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
	// Every other KModes is a mid-resolution ask, posed inside a tape run and
	// answered from its tape; one answered through Submit has no resolution
	// behind it.
	e.emit(events.Event{Kind: events.Note, Player: in.Player,
		Text: "modes answered with no resolution suspended"})
}

// recordModesAnswer is the answer record every mid-resolution KModes answer
// carries into the log when the resolution kernel serves the answer from
// its tape (resolveBoard.Record): the SetChosenMode Choose, the ModeChosen marker on
// the resolving object obj, and the ChoiceRestriction$ record.
func recordModesAnswer(e *Engine, d *decision.Decision, p state.PlayerID, chosen []decision.Option, obj state.ObjID) {
	if d.ResumeSA != nil && strings.EqualFold(d.ResumeSA.ParamStr(cards.PKSetChosenMode), "True") && len(chosen) == 1 {
		// An as-enters GenericChoice records its mode on the permanent via
		// the event fold; the ModeChosen marker alone stores no object state.
		if names := modeChoiceNames(d.ResumeSA, chosen, d.ResumeModes); len(names) == 1 {
			e.emit(events.Event{Kind: events.Choose, Obj: d.Source, Counter: "mode", Text: names[0]})
		}
	}
	e.emit(events.Event{Kind: events.ModeChosen, Obj: obj, Player: p,
		Text: strings.Join(chosenModeLabels(chosen), ",")})
	// ChoiceRestriction$: a mid-resolution Charm's pick is recorded on its
	// source as well, so a later instance is restricted against it.
	if o := e.G.Obj(obj); o != nil {
		effects.RecordCharmChoices(e, o.Source, d.ResumeSA,
			modeChoiceNames(d.ResumeSA, chosen, d.ResumeModes))
	}
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
	// CR 303.4f: a non-cast Aura's "what it enchants" answer (its first
	// as-enters election) is recorded on the entry record, which the
	// re-emitted move's fold attaches (rules/aura_entry.go); the Attach event
	// is the logged record. No option kind of its own reaches the switch.
	answerAuraEntry(e, &move, &opt)
	switch resumeETBEntryCodes.Code(string(opt.Kind)) {
	case resumeETBEntryName:
		e.emit(events.Event{Kind: events.Choose, Obj: move.Obj, Counter: "name", Text: opt.Label})
	case resumeETBEntryType:
		e.emit(events.Event{Kind: events.Choose, Obj: move.Obj, Counter: "type", Text: opt.Label})
	case resumeETBEntryNumber:
		e.emit(events.Event{Kind: events.Choose, Obj: move.Obj, Counter: "number", Amount: int32(opt.Amount)})
	case resumeETBEntryColor:
		if letter := etbColourLetter(opt.Label); letter != "" {
			e.emit(events.Event{Kind: events.Choose, Obj: move.Obj, Counter: "color", Text: letter})
		}
	case resumeETBEntryEvenOdd:
		quality := strings.ToLower(opt.Label)
		if quality == "odd" || quality == "even" {
			e.emit(events.Event{Kind: events.Choose, Obj: move.Obj, Counter: "type", Text: quality})
		}
	case resumeETBEntryRiot:
		choice := "haste"
		if opt.Index == 0 {
			choice = "counter"
		}
		e.emit(events.Event{Kind: events.Choose, Obj: move.Obj, Counter: "riot", Text: choice})
	case resumeETBEntryUnleash:
		choice := "plain"
		if opt.Index == 0 {
			choice = "counter"
		}
		e.emit(events.Event{Kind: events.Choose, Obj: move.Obj, Counter: "unleash", Text: choice})
	case resumeETBEntryClone:
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
	case resumeETBEntryPayLife:
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

func unlessPayChoice(chosen []decision.Option) (decision.Option, bool) {
	for _, option := range chosen {
		if option.Mode == decision.ModeUnlessPay {
			return option, true
		}
	}
	return decision.Option{}, false
}

type resumeETBEntryCode uint16

const (
	resumeETBEntryName resumeETBEntryCode = iota + 1
	resumeETBEntryType
	resumeETBEntryNumber
	resumeETBEntryColor
	resumeETBEntryEvenOdd
	resumeETBEntryRiot
	resumeETBEntryUnleash
	resumeETBEntryClone
	resumeETBEntryPayLife
)

var resumeETBEntryCodes = state.NewStrCodes(
	state.StrEntry[resumeETBEntryCode]{Key: "name", Val: resumeETBEntryName},
	state.StrEntry[resumeETBEntryCode]{Key: "type", Val: resumeETBEntryType},
	state.StrEntry[resumeETBEntryCode]{Key: "number", Val: resumeETBEntryNumber},
	state.StrEntry[resumeETBEntryCode]{Key: "color", Val: resumeETBEntryColor},
	state.StrEntry[resumeETBEntryCode]{Key: "evenodd", Val: resumeETBEntryEvenOdd},
	state.StrEntry[resumeETBEntryCode]{Key: "riot", Val: resumeETBEntryRiot},
	state.StrEntry[resumeETBEntryCode]{Key: "unleash", Val: resumeETBEntryUnleash},
	state.StrEntry[resumeETBEntryCode]{Key: "clone", Val: resumeETBEntryClone},
	state.StrEntry[resumeETBEntryCode]{Key: "paylife", Val: resumeETBEntryPayLife},
)
