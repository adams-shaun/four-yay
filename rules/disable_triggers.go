package rules

import (
	"sort"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// disableTriggersExcludes implements the printed DisableTriggers static at
// trigger admission. The receiver is deliberately the trigger-walk engine:
// leaves-the-battlefield checks use their observer's pre-event board here.
func (e *Engine) disableTriggersExcludes(t cards.Trigger, source state.ObjID, ev events.Event) bool {
	for _, sv := range e.activeStatics("DisableTriggers") {
		// ValidTrigger is the synthesized Ward shape, not a printed trigger
		// line. It is explicitly out of scope; fail open with a replay-visible
		// diagnostic rather than treating its ValidCard$ as a blanket gate.
		if _, hasValidTrigger := sv.Params["ValidTrigger"]; hasValidTrigger {
			e.emit(events.Event{Kind: events.Note, Obj: sv.Source,
				Text: "unmodelled DisableTriggers parameter: ValidTrigger (synthesized trigger)"})
			continue
		}
		// Any other parameter outside the supported grammar fails open, with
		// a replay-visible diagnostic rather than a blanket suppression.
		known := map[string]bool{
			"Mode": true, "Description": true, "ValidCause": true,
			"ValidMode": true, "Destination": true, "Origin": true,
			"ValidCard": true, "Secondary": true,
		}
		var unread []string
		for key := range sv.Params {
			if !known[key] {
				unread = append(unread, key)
			}
		}
		if len(unread) != 0 {
			sort.Strings(unread)
			e.emit(events.Event{Kind: events.Note, Obj: sv.Source,
				Text: "unmodelled DisableTriggers parameters: " + strings.Join(unread, ",")})
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
