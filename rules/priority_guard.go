package rules

import (
	"fmt"
	"strconv"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// A submitted-but-inert priority action is the livelock class cardfuzz kept
// finding (batch7 line 2: a granted ability the activation could not
// resolve; batch9 line 1: a mana ability offered off a face-down grantor):
// the offer walk (legalActions) and the handler behind the chosen option
// disagreed, the handler returned without an event, priority came straight
// back with the identical option list, and a seat that re-chose the option
// spun until the livelock watcher fired. Two guards close it.
//
//  1. validatePriorityChoice (Submit, before the intent is recorded): the
//     handler's own first-level anchor guards -- the object is gone, the
//     mana ability set is empty, the granted/gained/keyword/pile anchor no
//     longer resolves, a modal land has no back face, a Room has no locked
//     half, a spacecraft has no station candidate -- are checked with the
//     SAME predicates the handlers use, and a stale choice is rejected with
//     an error while the decision stays pending (validateAttackers' shape).
//     A driver records "error" rather than a 100-cycle livelock.
//
//  2. The inert backstop (handlePriority): whatever deeper path still
//     returns without emitting anything but the priority reset is recorded
//     as a Note carrying InertPriorityNotePrefix, and that exact option is
//     held out of the re-offer until the game next changes state (the
//     suppressedCast lifetime, see emit). The match never dies and never
//     spins; cardfuzz reads the Note and reports the game.

// InertPriorityNotePrefix starts the Note the inert backstop emits when a
// chosen priority action changed nothing. Drivers (cardfuzz) treat a log
// carrying it as a failure: it always names an offer/handler disagreement.
const InertPriorityNotePrefix = "inert priority action held out: "

// inertKey identifies one priority option across re-offers. Index is not
// part of it (the list is rebuilt every window); the label distinguishes two
// abilities of one object.
type inertKey struct {
	kind  string
	obj   state.ObjID
	label string
}

func inertKeyOf(o decision.Option) inertKey {
	return inertKey{kind: o.Kind, obj: o.Obj, label: o.Label}
}

// validatePriorityChoice rejects a priority answer whose handler would
// return at its first guard without emitting anything. Every predicate here
// is the one the handler itself runs, so an option the handler can act on is
// never rejected.
func (e *Engine) validatePriorityChoice(d *decision.Decision, in decision.Intent) error {
	if len(in.Choices) == 0 {
		return nil
	}
	opt := d.Options[in.Choices[0]]
	if reason := e.priorityOptionStale(in.Player, opt); reason != "" {
		return fmt.Errorf("priority option %d (%s %q) cannot be performed: %s", opt.Index, opt.Kind, opt.Label, reason)
	}
	return nil
}

// priorityOptionStale returns why opt's handler would no-op at its first
// guard, or "" when the handler proceeds.
func (e *Engine) priorityOptionStale(p state.PlayerID, opt decision.Option) string {
	switch opt.Kind {
	case "pass", "concede":
		return ""
	}
	o := e.G.Obj(opt.Obj)
	switch opt.Kind {
	case "activate":
		// activateMana -> activateManaFor's own member set.
		if len(e.availableManaAbilitiesForWindow(p, opt.Obj, true)) == 0 {
			return "the source has no activatable mana ability"
		}
	case "ability":
		if o == nil {
			return "the object no longer exists"
		}
		switch {
		case opt.GainedSource != 0:
			// beginGainedActivation's anchor.
			if o.Zone != state.ZBattlefield || o.Face() == nil {
				return "the recipient left the battlefield"
			}
			fo := e.G.Obj(opt.GainedSource)
			if fo == nil || fo.Face() == nil || opt.GainedIdx < 0 || opt.GainedIdx >= len(fo.Face().Abilities) ||
				fo.Face().Abilities[opt.GainedIdx] == nil {
				return "the gained ability no longer resolves"
			}
		case opt.SVar != "":
			return e.grantedAnchorStale(o, opt)
		case opt.Keyword != "":
			// beginKeywordGrantedActivation's anchor.
			if o.Face() == nil || cards.GrantedCyclingAbility(opt.Keyword) == nil {
				return "the keyword-granted ability no longer resolves"
			}
		default:
			// beginActivation's printed-pile anchor.
			if o.Face() == nil {
				return "the object has no face"
			}
			if _, ok := o.PileAbilityAt(opt.Ability); !ok {
				return "the ability index no longer resolves"
			}
		}
	case "granted":
		if o == nil {
			return "the object no longer exists"
		}
		return e.grantedAnchorStale(o, opt)
	case "play_land":
		if opt.Mode == "modal_land" && modalLandBack(o) == nil {
			return "the land has no modal back face"
		}
	case "unlock":
		if _, ok := e.unlockRoomCost(o); !ok {
			return "the Room has no locked half to unlock"
		}
	case "specialize":
		i, err := strconv.Atoi(opt.Mode)
		if err != nil {
			return "the chosen specialization face is invalid"
		}
		if _, ok := e.specializeLegal(p, opt.Obj, i); !ok {
			return "the permanent cannot be specialized to that face"
		}
	case "station":
		if o == nil || o.Zone != state.ZBattlefield || !e.HasKeyword(opt.Obj, "Station") {
			return "the spacecraft cannot be stationed"
		}
		if len(e.stationCandidates(p, opt.Obj)) == 0 {
			return "no creature can be tapped to station it"
		}
	case "cast":
		if o == nil {
			return "the card no longer exists"
		}
	case "turn_face_up":
		// rules/morph_turnup.go's morphFaceUpCost, the same predicate the
		// offer and the handler run: the permanent must still be a face-down
		// battlefield permanent the morph family put down (the family flag),
		// with a printed keyword parameter to price the action.
		if _, ok := morphFaceUpCost(o); !ok {
			return "the permanent is no longer a face-down morph-family permanent"
		}
		if e.turnFaceUpCantHappen(o.ID) {
			return "the turn-up is prevented by a CantHappen replacement effect (CR 614.1a)"
		}
	}
	return ""
}

// grantedAnchorStale is beginGrantedActivation's first guard pair.
func (e *Engine) grantedAnchorStale(o *state.Object, opt decision.Option) string {
	if o.Zone != state.ZBattlefield || o.Face() == nil {
		return "the recipient left the battlefield"
	}
	grantor := opt.GrantSource
	if grantor == 0 {
		grantor = opt.Obj
	}
	if e.grantedSAFrom(grantor, opt.Obj, opt.SVar) == nil {
		return "the granted ability no longer resolves"
	}
	return ""
}

// inertPriorityBackstop runs after a non-pass priority handler. mark is the
// log length before the handler ran. When the handler emitted nothing but
// Priority events, posed no decision and left nothing in flight, the action
// was inert: record it loudly and hold the option out of the re-offer.
func (e *Engine) inertPriorityBackstop(p state.PlayerID, opt decision.Option, mark int) {
	if e.pending != nil || e.cast != nil || e.G.Over {
		return
	}
	for _, ev := range e.L.Events[mark:] {
		if ev.Kind != events.Priority {
			return
		}
	}
	e.emit(events.Event{Kind: events.Note, Player: p, Obj: opt.Obj,
		Text: InertPriorityNotePrefix + opt.Kind + " " + opt.Label})
	if e.inertHeldOut == nil {
		e.inertHeldOut = map[inertKey]bool{}
	}
	e.inertHeldOut[inertKeyOf(opt)] = true
}

// filterInertHeldOut drops the options the inert backstop is holding out of
// the current window and re-indexes the rest. A nil set (every ordinary
// window) returns out untouched.
func (e *Engine) filterInertHeldOut(out []decision.Option) []decision.Option {
	if len(e.inertHeldOut) == 0 {
		return out
	}
	kept := out[:0]
	for _, o := range out {
		if e.inertHeldOut[inertKeyOf(o)] {
			continue
		}
		o.Index = len(kept)
		kept = append(kept, o)
	}
	return kept
}
