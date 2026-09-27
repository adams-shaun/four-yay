package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

func TestCountCommanderCastFromCommandZone(t *testing.T) {
	h, base := fixtureHost(t)
	c := *base
	h.commanderCasts = map[state.PlayerID]int32{0: 2, 1: 1}

	if got, ok := EvalCountOK(h, &c, "Count$CommanderCastFromCommandZone"); !ok || got != 2 {
		t.Fatalf("seat 0 count = (%d, %v), want (2, true)", got, ok)
	}
	c.Controller = 1
	if got, ok := EvalCountOK(h, &c, "Count$CommanderCastFromCommandZone"); !ok || got != 1 {
		t.Fatalf("seat 1 count = (%d, %v), want (1, true)", got, ok)
	}
	c.Controller = 2
	if got, ok := EvalCountOK(h, &c, "Count$CommanderCastFromCommandZone"); !ok || got != 0 {
		t.Fatalf("zero count = (%d, %v), want (0, true)", got, ok)
	}
	if got, ok := EvalCountOK(h, &c, "Count$CommanderCastFromCommandZone/Plus.1"); !ok || got != 1 {
		t.Fatalf("/Plus.1 zero count = (%d, %v), want (1, true)", got, ok)
	}
	// Existing spelling remains independently covered by its existing test;
	// pin it here as well to ensure both names retain the same host semantics.
	if got, ok := EvalCountOK(h, &c, "Count$TotalCommanderCastFromCommandZone"); !ok || got != 0 {
		t.Fatalf("Total zero count = (%d, %v), want (0, true)", got, ok)
	}
}
