package rules

import (
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func recordDamageProvenance(emit func(events.Event) events.Event, source, recipient state.ObjID, amount int32, combat bool) {
	text := ""
	if combat {
		text = events.DamageProvenanceCombat
	}
	emit(events.Event{Kind: events.DamageProvenance, Obj: source, IDs: []state.ObjID{recipient}, Amount: amount, Text: text})
}
