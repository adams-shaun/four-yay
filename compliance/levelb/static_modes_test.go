package levelb_test

import (
	"testing"

	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// reqOf finds one static requirement by key and asserts its classification:
// the sub-family the template serves and no gap -- so a classifier regression
// (a shape that stops matching, or a shape that starts matching too widely)
// fails here before the template is ever asked for an item.
func reqOf(t *testing.T, card, key, sub string) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	c, ok := reg.Lookup(card)
	if !ok {
		t.Fatalf("precondition: %s absent from corpus", card)
	}
	for _, req := range levelb.Requirements(c) {
		if req.Key != key {
			continue
		}
		if req.Sub != sub || req.Gap != "" {
			t.Fatalf("%s %s classifies as %q gap %q, want %q gap \"\"", card, key, req.Sub, req.Gap, sub)
		}
		return
	}
	t.Fatalf("precondition: %s has no requirement %s", card, key)
}

func TestStaticModeShapesClassify(t *testing.T) {
	for _, tc := range []struct {
		card, key, sub string
	}{
		// TapPowerValue: the Pilot cycle crews with its power plus Value$.
		{"Cloudspire Captain", "static#0.1", "static.tap-power-value"},
		{"Deathless Pilot", "static#0.0", "static.tap-power-value"},
		{"Interface Ace", "static#0.0", "static.tap-power-value"},
		// CastWithFlash: other spells and the card's own conditional cast.
		{"Valley Floodcaller", "static#0.0", "static.cast-with-flash"},
		{"Whirlwing Stormbrood", "static#0.0", "static.cast-with-flash"},
		{"Radagast of Rhosgobel", "static#0.1", "static.cast-with-flash"},
		{"Illusion Spinners", "static#0.0", "static.cast-with-flash"},
		{"Serpent of the Pass", "static#0.0", "static.cast-with-flash"},
		// UntapOtherPlayer: the card itself, and each creature you control.
		{"Thousand Moons Infantry", "static#0.0", "static.untap-other-player"},
		{"Bender's Waterskin", "static#0.0", "static.untap-other-player"},
		// CantDraw and the widened CantGainLife shape (no ValidPlayer$, a
		// Secondary rider).
		{"Mornsong Aria", "static#0.0", "static.cant-draw"},
		{"Mornsong Aria", "static#0.1", "static.cant-gain-life"},
	} {
		reqOf(t, tc.card, tc.key, tc.sub)
	}
}

// TestRoomDoorStaticServable asserts the face-1 statics of the DSK Rooms are
// served by the door-cast path: the classified sub-family stands (no face
// gap) for the sub-families RoomStaticServable admits.
func TestRoomDoorStaticServable(t *testing.T) {
	for _, tc := range []struct {
		card, key, sub string
	}{
		{"Mirror Room // Fractured Realm", "static#1.0", "static.panharmonicon"},
		{"Dollmaker's Shop // Porcelain Gallery", "static#1.0", "static.continuous"},
		{"Restricted Office // Lecture Hall", "static#1.0", "static.continuous"},
		{"Dazzling Theater // Prop Room", "static#1.0", "static.untap-other-player"},
	} {
		reqOf(t, tc.card, tc.key, tc.sub)
	}
}
