package rules

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func recordDamageProvenance(emit func(events.Event) events.Event, source, recipientObject *state.Object, recipient state.ObjID, amount int32, combat bool, sourceColors string, derived []effects.ObjectTypes) {
	if source == nil {
		return
	}
	text := ""
	if combat {
		text = events.DamageProvenanceCombat
	}
	text += events.DamageProvenanceColorSeparator + sourceColors
	if recipientObject != nil {
		if types := damageSnapshotTypes(recipient, recipientObject, derived); types != nil {
			text += events.DamageProvenanceTypeSeparator + strings.Join(types, events.DamageProvenanceTypeWordSeparator)
		}
	}
	// The layer-4 table is available only in rules, not in events.Apply. Store
	// the complete damage-time type words, materializing an all-types marker
	// into its vocabulary so the event remains a faithful historical snapshot.
	types := damageSnapshotTypes(source.ID, source, derived)
	text += events.DamageProvenanceSourceSeparator + strconv.Itoa(int(source.Zone)) +
		events.DamageProvenanceSourceTypeSeparator + strings.Join(types, events.DamageProvenanceTypeWordSeparator)
	emit(events.Event{Kind: events.DamageProvenance, Obj: source.ID, IDs: []state.ObjID{recipient}, Amount: amount, Text: text})
}

func damageSnapshotTypes(id state.ObjID, object *state.Object, derived []effects.ObjectTypes) []string {
	var types []string
	all := false
	found := false
	for _, entry := range derived {
		if entry.ID == id {
			types = entry.Types
			all = entry.AllCreatureTypes
			found = true
			break
		}
	}
	if !found {
		if face := object.Face(); face != nil {
			types = face.Types
			all = effects.IntrinsicAllCreatureTypes(object)
		}
	}
	if all {
		out := append([]string(nil), types...)
		for _, word := range effects.CreatureTypeWordList() {
			present := false
			for _, typ := range out {
				if strings.EqualFold(typ, word) {
					present = true
					break
				}
			}
			if !present {
				out = append(out, word)
			}
		}
		return out
	}
	return types
}
