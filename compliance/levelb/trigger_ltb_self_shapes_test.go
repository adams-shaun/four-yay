package levelb

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
)

// TestSelfLTBShapes pins the boundary of the sub-family on synthetic faces:
// exact Graveyard stays self-dies, exact Battlefield stays an ETB, a static
// cleanup is a named gap, and a non-Battlefield origin is not an LTB.
func TestSelfLTBShapes(t *testing.T) {
	for _, tc := range []struct {
		name, want, gap string
		params          map[string]string
	}{
		{"any destination", "trigger.ltb-self", "", map[string]string{"Origin": "Battlefield", "Destination": "Any", "ValidCard": "Card.Self"}},
		{"no destination", "trigger.ltb-self", "", map[string]string{"Origin": "Battlefield", "ValidCard": "Card.Self"}},
		{"graveyard and exile", "trigger.ltb-self", "", map[string]string{"Origin": "Battlefield", "Destination": "Graveyard,Exile", "ValidCard": "Card.Self+powerGE4"}},
		{"self or other", "trigger.ltb-self", "", map[string]string{"Origin": "Battlefield", "Destination": "Exile,Hand", "ValidCard": "Card.Self,Creature.Other+YouCtrl"}},
		{"self dies stays dies", "trigger.dies", "", map[string]string{"Origin": "Battlefield", "Destination": "Graveyard", "ValidCard": "Card.Self"}},
		{"static cleanup is named", "trigger.gap:ChangesZone", "static trigger", map[string]string{"Origin": "Battlefield", "Destination": "Any", "ValidCard": "Card.Self", "Static": "True"}},
		{"graveyard origin is not an LTB", "trigger.gap:ChangesZone", "trigger mode ChangesZone", map[string]string{"Origin": "Graveyard", "Destination": "Exile", "ValidCard": "Card.Self"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := Requirements(cardOf(&cards.Face{Types: []string{"Creature"}, Triggers: []cards.Trigger{trig("ChangesZone", tc.params)}}))
			if len(got) != 1 {
				t.Fatalf("precondition: got %d requirements, want 1", len(got))
			}
			if got[0].Sub != tc.want || got[0].Gap != tc.gap {
				t.Fatalf("sub=%q gap=%q, want sub=%q gap=%q", got[0].Sub, got[0].Gap, tc.want, tc.gap)
			}
		})
	}
}
