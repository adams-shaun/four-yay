package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestMalformedWithCountersAmountIsLoudNotSilent guards reviewer item 1: a
// malformed WithCountersAmount$ (bad integer) must surface a deterministic
// Note event -- the same mechanism Resolve uses for an unimplemented API,
// so it is replay-log-visible -- rather than silently defaulting to 1 with
// no trace. The move still happens with the safe default 1, but the board
// mismatch is no longer invisible.
func TestMalformedWithCountersAmountIsLoudNotSilent(t *testing.T) {
	h, c := fixtureHost(t)
	// Give the sourced fixture an LKI so Matches### doesn't gate the effect;
	// ChangeZone with no Origin falls back to the object's current zone.
	h.g.Obj(c.Source).Zone = state.ZHand

	c.SVars = map[string]string{}
	sa := &cards.SA{API: "ChangeZone", Params: map[string]string{
		"Defined":            "Self",
		"Destination":        "Battlefield",
		"WithCountersType":   "P1P1",
		"WithCountersAmount": "not-a-number",
	}}
	effChangeZone(h, c, sa)

	var notes []events.Event
	for _, e := range h.log {
		if e.Kind == events.Note {
			notes = append(notes, e)
		}
	}
	if len(notes) != 1 {
		t.Fatalf("expected exactly one Note for malformed WithCountersAmount, got %d: %+v", len(notes), h.log)
	}
	if want := "malformed WithCountersAmount not-a-number"; notes[0].Text != want {
		t.Fatalf("Note.Text = %q, want %q", notes[0].Text, want)
	}
	// The move still landed, and the counter defaulted to the safe 1.
	o := h.g.Obj(c.Source)
	if o.Zone != state.ZBattlefield {
		t.Fatalf("object did not move to battlefield: zone %v", o.Zone)
	}
	if o.Counter("P1P1") != 1 {
		t.Fatalf("counter = %d, want safe default 1", o.Counter("P1P1"))
	}
}
