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

func TestCountedRepeatAfterBodyResumeRunsTheRest(t *testing.T) {
	bodyRuns := 0
	Register("TestCountedRepeatResumeBody", func(Host, *Ctx, *cards.SA) { bodyRuns++ })
	t.Cleanup(func() { unregister("TestCountedRepeatResumeBody") })

	h, c := fixtureHost(t)
	c.SVars = map[string]string{"Body": "DB$ TestCountedRepeatResumeBody"}
	// The recorded bound (3) wins over a MaxRepeat$ that now reads 9: Forge
	// computes the count once, before the first iteration.
	c.RepeatResume = &RepeatContinuation{Continue: true, Next: 1, AfterBody: true, Count: 3}
	Resolve(h, c, &cards.SA{Kind: "DB", API: "Repeat", Params: map[string]string{
		"RepeatSubAbility": "Body", "MaxRepeat": "9",
	}})
	if bodyRuns != 2 {
		t.Fatalf("the resumed counted Repeat ran its body %d times, want 2 (iterations 1 and 2 of 3)", bodyRuns)
	}
	if h.askCount != 0 {
		t.Fatalf("the resumed counted Repeat posed %d asks, want 0 (no election is owed)", h.askCount)
	}
	if c.RepeatResume != nil {
		t.Fatalf("Ctx.RepeatResume = %+v after the walk, want consumed", c.RepeatResume)
	}
}
