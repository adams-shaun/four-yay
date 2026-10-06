package levelb

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
)

// roomFace is a Room half with one trigger.
func roomFace(name string, tr cards.Trigger) *cards.Face {
	return &cards.Face{Name: name, Types: []string{"Enchantment", "Room"}, Triggers: []cards.Trigger{tr}}
}

// roomCard is a two-door Room whose doors carry the given triggers.
func roomCard(mode string, a, b cards.Trigger) *cards.Card {
	return &cards.Card{AlternateMode: mode, Faces: []*cards.Face{roomFace("Front", a), roomFace("Back", b)}}
}

// TestRoomUnlockTriggerSubFamilies pins the Room trigger classification: the
// door's own "when you unlock this door" and Eerie's "whenever you fully unlock
// a Room" are named sub-families, and any other filter stays a gap.
func TestRoomUnlockTriggerSubFamilies(t *testing.T) {
	for _, tc := range []struct {
		name, mode string
		params     map[string]string
		wantSub    string
		wantGap    bool
	}{
		{"Bottomless Pool", "UnlockDoor", map[string]string{"ValidPlayer": "You", "ValidCard": "Card.Self", "ThisDoor": "True"}, UnlockDoorSub, false},
		{"Balemurk Leech", "FullyUnlock", map[string]string{"ValidCard": "Card.Room", "ValidPlayer": "You"}, FullyUnlockSub, false},
		{"opponent unlocks", "UnlockDoor", map[string]string{"ValidPlayer": "Opponent", "ValidCard": "Card.Self"}, "trigger.gap:UnlockDoor", true},
		{"any Room's door", "UnlockDoor", map[string]string{"ValidPlayer": "You", "ValidCard": "Card.Room"}, "trigger.gap:UnlockDoor", true},
		{"opponent fully unlocks", "FullyUnlock", map[string]string{"ValidCard": "Card.Room", "ValidPlayer": "Opponent"}, "trigger.gap:FullyUnlock", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := Requirements(cardOf(&cards.Face{Types: []string{"Creature"}, Triggers: []cards.Trigger{trig(tc.mode, tc.params)}}))
			if len(got) != 1 {
				t.Fatalf("got %d requirements, want 1: %+v", len(got), got)
			}
			if r := got[0]; r.Sub != tc.wantSub || (r.Gap != "") != tc.wantGap {
				t.Fatalf("got Sub=%q Gap=%q, want Sub=%q gap=%v", r.Sub, r.Gap, tc.wantSub, tc.wantGap)
			}
		})
	}
}

// TestRoomSecondDoorTriggerGapIsLifted pins that a trigger on a Room's second
// door is served (its template casts the door), while the same shape on any
// other split layout, and a non-trigger requirement on a Room's second door,
// keep the face gap.
func TestRoomSecondDoorTriggerGapIsLifted(t *testing.T) {
	phase := trig("Phase", map[string]string{"Phase": "End of Turn", "ValidPlayer": "You"})
	unlock := trig("UnlockDoor", map[string]string{"ValidPlayer": "You", "ValidCard": "Card.Self", "ThisDoor": "True"})

	room := roomCard("Split", phase, unlock)
	for _, key := range []string{"trigger#0.0", "trigger#1.0"} {
		if r := reqByKey(t, room, key); r.Gap != "" {
			t.Errorf("Room %s gap = %q, want none", key, r.Gap)
		}
	}
	if r := reqByKey(t, room, "trigger#1.0"); r.Sub != UnlockDoorSub {
		t.Errorf("Room trigger#1.0 sub = %q, want %q", r.Sub, UnlockDoorSub)
	}

	// An Adventure keeps the gap: only a Room's second door is castable by name.
	adv := roomCard("Adventure", phase, unlock)
	for _, f := range adv.Faces {
		f.Types = []string{"Creature"}
	}
	if r := reqByKey(t, adv, "trigger#1.0"); r.Gap != "face 1" {
		t.Errorf("Adventure trigger#1.0 gap = %q, want %q", r.Gap, "face 1")
	}

	// A Room's second-door activated ability is not served by the cast recipes.
	room.Faces[1].Abilities = []*cards.SA{{Kind: "AB", API: "Pump"}}
	if r := reqByKey(t, room, "activate#1.0"); r.Gap != "face 1" {
		t.Errorf("Room activate#1.0 gap = %q, want %q", r.Gap, "face 1")
	}
}

func reqByKey(t *testing.T, c *cards.Card, key string) Requirement {
	t.Helper()
	for _, r := range Requirements(c) {
		if r.Key == key {
			return r
		}
	}
	t.Fatalf("no requirement %q in %+v", key, Requirements(c))
	return Requirement{}
}
