package rules

import (
	"sort"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// disableTriggersKnownParams is the complete parameter grammar this build
// reads on a printed S:Mode$ DisableTriggers line. A parameter outside this
// set fails the whole static OPEN (see disableTriggersUnread), so an
// unmodelled shape can never blanket-suppress triggers; the live trigger walk
// reports the unread parameter once per game (rules/trigger_match.go), the
// effects/misc.go "unmodelled ..." convention.
var disableTriggersKnownParams = map[string]bool{
	"Mode": true, "Description": true, "ValidCause": true,
	"ValidMode": true, "Destination": true, "Origin": true,
	"ValidCard": true, "Secondary": true,
}

// disableTriggersUnread returns the sorted parameter names on a DisableTriggers
// static that this build does not read, or nil when every parameter is known.
//
// ValidTrigger$ (Nowhere to Run's synthesized-Ward shape) is deliberately NOT
// in disableTriggersKnownParams: the Ward trigger is built by
// checkGrantedWardTriggers, never a printed T: line, so nothing here can
// suppress it. Naming it unknown makes that line fail open with a report,
// rather than silently blanket-suppressing the carrying creature's own
// printed triggers.
func disableTriggersUnread(sv staticView) []string {
	var unread []string
	for key := range sv.Params {
		if !disableTriggersKnownParams[key] {
			unread = append(unread, key)
		}
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
	for _, sv := range e.activeStatics("DisableTriggers") {
		if len(disableTriggersUnread(sv)) != 0 {
			continue
		}
		if spec, ok := sv.Param(cards.PKValidCause); ok {
			if ev.Obj == 0 || !e.matchesSpec(spec, ev.Obj, e.specCtx(sv.Source, sv.Controller)) {
				continue
			}
		}
		if raw := sv.Params["ValidMode"]; raw != "" {
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
		if raw, ok := sv.Param(cards.PKDestination); ok && raw != "Any" {
			zone, valid := effects.ParseZoneWord(raw)
			if !valid || ev.To != zone {
				continue
			}
		}
		if raw, ok := sv.Param(cards.PKOrigin); ok && raw != "Any" {
			zone, valid := effects.ParseZoneWord(raw)
			if !valid || ev.From != zone {
				continue
			}
		}
		if spec, ok := sv.Param(cards.PKValidCard); ok &&
			!e.matchesSpec(spec, source, e.specCtx(sv.Source, sv.Controller)) {
			continue
		}
		return true
	}
	return false
}
