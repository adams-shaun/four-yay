package rules

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func recordDamageProvenance(emit func(events.Event) events.Event, source *state.Object, recipient state.ObjID, amount int32, combat bool, sourceColors string, derived []effects.ObjectTypes) {
	if source == nil {
		return
	}
	text := ""
	if combat {
		text = events.DamageProvenanceCombat
	}
	text += events.DamageProvenanceColorSeparator + sourceColors
	if types := damageRecipientDerivedTypes(recipient, derived); types != nil {
		text += events.DamageProvenanceTypeSeparator + strings.Join(types, events.DamageProvenanceTypeWordSeparator)
	}
	// The layer-4 table is available only in rules, not in events.Apply.
	types := damageRecipientDerivedTypes(source.ID, derived)
	if types == nil {
		if face := source.Face(); face != nil {
			types = face.Types
		}
	}
	text += events.DamageProvenanceSourceSeparator + strconv.Itoa(int(source.Zone)) +
		events.DamageProvenanceSourceTypeSeparator + strings.Join(types, events.DamageProvenanceTypeWordSeparator)
	emit(events.Event{Kind: events.DamageProvenance, Obj: source.ID, IDs: []state.ObjID{recipient}, Amount: amount, Text: text})
}

func damageRecipientDerivedTypes(recipient state.ObjID, derived []effects.ObjectTypes) []string {
	for _, entry := range derived {
		if entry.ID == recipient {
			return entry.Types
		}
	}
	return nil
}
