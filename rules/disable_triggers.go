package rules

import (
	"sort"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// stat:DisableTriggers is enforced at the shared trigger-admission gate
// (disableTriggersExcludes, called from triggerMatchesWithSVars) for every
// printed corpus shape: ValidCause$/ValidCard$/ValidMode$/Destination$/
// Origin$/Secondary$ and ValidTrigger$ Triggered.Ward. An unread parameter
// fails the static open with a loud Note, never a blanket suppression.
func init() { effects.RegisterNonAPI("stat:DisableTriggers") }

// disableTriggersKnownParams is the complete parameter grammar this build
// reads on a printed S:Mode$ DisableTriggers line. A parameter outside this
// set fails the whole static OPEN (see disableTriggersUnread), so an
// unmodelled shape can never blanket-suppress triggers; the live trigger walk
// reports the unread parameter once per game (rules/trigger_match.go), the
// effects/misc.go "unmodelled ..." convention.
var disableTriggersKnownParams = map[string]bool{
	"Mode": true, "Description": true, "ValidCause": true,
	"ValidMode": true, "Destination": true, "Origin": true,
	"ValidCard": true, "ValidTrigger": true, "Secondary": true,
}

// disableTriggersUnread returns the sorted parameter names on a DisableTriggers
// static that this build does not read, or nil when every parameter is known.
//
// ValidTrigger$ is read for the one qualifier the corpus carries,
// Triggered.Ward: the Ward trigger is built by checkGrantedWardTriggers, never
// a printed T: line, and the engine marks it with Params["Ward"] == "True".
// A ValidTrigger$ VALUE this build does not model is reported as
// "ValidTrigger=<value>" here, so it fails the static OPEN with the live
// walk's loud unmodelled Note rather than silently blanket-suppressing the
// carrying creature's own printed triggers; triggerKindMatches then also
// fails closed as a second guard.
func disableTriggersUnread(sv staticView) []string {
	var unread []string
	for key := range sv.Params {
		if !disableTriggersKnownParams[key] {
			unread = append(unread, key)
		}
	}
	if raw := sv.ParamStr(cards.PKValidTrigger); raw != "" && raw != "Triggered.Ward" {
		unread = append(unread, "ValidTrigger="+raw)
	}
	if len(unread) == 0 {
		return nil
	}
	sort.Strings(unread)
	return unread
}

// disableTriggersExcludes reports whether a printed DisableTriggers static on
// the battlefield suppresses trigger t of source for event ev. It is the
// trigger-admission gate every printed trigger line passes through
// (triggerMatchesWithSVars).
//
// Two distinct objects are matched, never conflated:
//
//   - ValidCause$ names the CAUSE of the event that would trigger -- the
//     permanent that entered or died -- so it matches ev.Obj.
//   - ValidCard$ names WHICH permanent's abilities are suppressed -- the
//     trigger's own source -- so it matches source. Elesh Norn's
//     "Permanent.OppCtrl+inZoneBattlefield" scopes opponents' permanents.
//
// Destination$/Origin$ restrict the event's zone transition and are read from
// ev.To/ev.From with ParseZoneWord, whose absent/Any form means no
// restriction. ValidMode$ restricts which trigger Mode$s are suppressed.
// Secondary$ is treated as an ordinary independent suppression: unlike a
// printed trigger pair, a DisableTriggers line has no paired primary, and both
// Hushbringer halves are separately meaningful.
//
// The receiver is deliberately the trigger-walk engine: leaves-the-battlefield
// checks use their observer's pre-event board here. The caller must not emit
// from this function; an unread static merely fails open (the walk reports it).
func (e *Engine) disableTriggersExcludes(t cards.Trigger, source state.ObjID, ev events.Event) bool {
	// A token mint (TokenCreate/CardToken) carries no move zones of its own:
	// its fold moves the token straight onto the battlefield, so it is read
	// as Library -> Battlefield here exactly as trigmatch.ZoneChangeMatches reads it --
	// otherwise a creature TOKEN entering would escape Karn, Argent
	// Defender's / Torpor Orb's Destination$ Battlefield while still firing
	// the ETB trigger it should suppress.
	from, to := ev.From, ev.To
	if ev.Kind == events.TokenCreate || ev.Kind == events.CardToken {
		from, to = state.ZLibrary, state.ZBattlefield
	}
	for _, sv := range e.activeStatics("DisableTriggers") {
		if len(disableTriggersUnread(sv)) != 0 {
			continue
		}
		if spec, ok := sv.Param(cards.PKValidCause); ok {
			if ev.Obj == 0 || !e.matchesSpec(spec, ev.Obj, e.specCtx(sv.Source, sv.Controller)) {
				continue
			}
		}
		if raw := sv.ParamStr(cards.PKValidMode); raw != "" {
			found := false
			for _, mode := range strings.Split(raw, ",") {
				if strings.TrimSpace(mode) == t.Mode {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}
		if code, ok := sv.ParamCode(cards.PKDestination); ok && !effects.Destination(code).Admits(to) {
			continue
		}
		if raw, ok := sv.Param(cards.PKOrigin); ok && raw != "Any" {
			zone, valid := effects.ParseZoneWord(raw)
			if !valid || from != zone {
				continue
			}
		}
		if spec, ok := sv.Param(cards.PKValidCard); ok &&
			!e.matchesSpec(spec, source, e.specCtx(sv.Source, sv.Controller)) {
			continue
		}
		if raw := sv.ParamStr(cards.PKValidTrigger); raw != "" && !triggerKindMatches(raw, t) {
			continue
		}
		return true
	}
	return false
}

// triggerKindMatches reports whether the trigger Kind qualifier on a
// DisableTriggers line's ValidTrigger$ names trigger t. Only
// "Triggered.Ward" -- Nowhere to Run's shape, the sole ValidTrigger$ value
// among the corpus's DisableTriggers carriers -- is modelled, and it names
// the Ward trigger checkGrantedWardTriggers synthesizes from the derived
// keyword list, which the engine marks with Params["Ward"] == "True"
// (rules/trigmatch/actions.go). Any other qualifier is unmodelled and fails
// closed, so an unknown value makes the whole static fail open rather than
// blanket-suppress every trigger of the matched creature -- the same
// discipline replacement_match.go's Triggered.Modular branch follows.
func triggerKindMatches(raw string, t cards.Trigger) bool {
	if raw != "Triggered.Ward" {
		return false
	}
	return t.ParamStr(cards.PKWard) == "True"
}
