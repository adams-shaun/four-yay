package levelb_test

import (
	"testing"

	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// TestRoomFaceOneGapIsLiftedForTheCorpusRooms is the TestFaceOneGapIsLifted
// pattern on real DSK Rooms: Bottomless Pool's second door (Locker Room) and
// Funeral Room's (Awakening Hall) triggers are no longer "face 1" gaps (Locker
// Room's is the DamageDoneOnce mode gap it would be on a front face). Non-Room
// layouts keep the face gap: TestRoomSecondDoorTriggerGapIsLifted.
func TestRoomFaceOneGapIsLiftedForTheCorpusRooms(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, tc := range []struct{ card, key, sub, gap string }{
		{"Bottomless Pool // Locker Room", "trigger#1.0", "trigger.gap:DamageDoneOnce", "trigger mode DamageDoneOnce"}, // a mode gap now, no longer the face gap
		{"Funeral Room // Awakening Hall", "trigger#1.0", levelb.UnlockDoorSub, ""},
		{"Bottomless Pool // Locker Room", "trigger#0.0", levelb.UnlockDoorSub, ""},
	} {
		c, ok := reg.Lookup(tc.card)
		if !ok {
			t.Fatalf("%s not in corpus", tc.card)
		}
		found := false
		for _, r := range levelb.Requirements(c) {
			if r.Key != tc.key {
				continue
			}
			found = true
			if r.Gap != tc.gap || r.Sub != tc.sub {
				t.Errorf("%s %s = sub %q gap %q, want sub %q gap %q", tc.card, tc.key, r.Sub, r.Gap, tc.sub, tc.gap)
			}
		}
		if !found {
			t.Errorf("%s has no requirement %s", tc.card, tc.key)
		}
	}
}
