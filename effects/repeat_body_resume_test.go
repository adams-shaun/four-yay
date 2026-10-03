package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
)

// A NON-optional Repeat whose body suspends must park its loop cursor and
// resolved bound exactly as RepeatOptional$ does (SuspendRepeatBody), and
// the AfterBody resume must run the remaining iterations with no election.
// Before the fix only RepeatOptional$ reported, so a counted or gated Repeat
// fell through to Repeat.Sub after the body's ask and dropped every later
// iteration (Remorseless Punishment's second process, Torment of
// Hailfire's X-1 remaining ones).

func TestCountedRepeatRecordsCursorWhenBodySuspends(t *testing.T) {
	Register("TestCountedRepeatSuspendingBody", func(Host, *Ctx, *cards.SA) {})
	t.Cleanup(func() { unregister("TestCountedRepeatSuspendingBody") })

	h, c := fixtureHost(t)
	h.suspendAfterAsk = true
	c.SVars = map[string]string{"Body": "DB$ TestCountedRepeatSuspendingBody"}
	Resolve(h, c, &cards.SA{Kind: "DB", API: "Repeat", Params: map[string]string{
		"RepeatSubAbility": "Body", "MaxRepeat": "3",
	}})
	if !h.repeatBodyCalled {
		t.Fatal("a counted Repeat's body suspension did not register its loop frame")
	}
	if h.repeatBodyNext != 1 || h.repeatBodyCount != 3 {
		t.Fatalf("loop frame cursor = %d count = %d, want 1 and 3", h.repeatBodyNext, h.repeatBodyCount)
	}
}
