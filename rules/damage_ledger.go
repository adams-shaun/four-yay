package rules

import (
	"strings"

	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func recordDamageProvenance(emit func(events.Event) events.Event, source, recipient state.ObjID, amount int32, combat bool, derived []effects.ObjectTypes) {
	text := ""
	if combat {
		text = events.DamageProvenanceCombat
	}
	if types := damageRecipientDerivedTypes(recipient, derived); types != nil {
		text += events.DamageProvenanceTypeSeparator + strings.Join(types, events.DamageProvenanceTypeWordSeparator)
	}
	emit(events.Event{Kind: events.DamageProvenance, Obj: source, IDs: []state.ObjID{recipient}, Amount: amount, Text: text})
}

func damageRecipientDerivedTypes(recipient state.ObjID, derived []effects.ObjectTypes) []string {
	for _, entry := range derived {
		if entry.ID == recipient {
			return entry.Types
		}
	}
	return nil
}
