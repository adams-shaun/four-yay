package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
)

func TestRepeatOptionalRecordsCursorWhenBodySuspends(t *testing.T) {
	Register("TestRepeatOptionalSuspendingBody", func(Host, *Ctx, *cards.SA) {})
	t.Cleanup(func() { unregister("TestRepeatOptionalSuspendingBody") })

	h, c := fixtureHost(t)
	h.suspendAfterAsk = true
	c.SVars = map[string]string{"Body": "DB$ TestRepeatOptionalSuspendingBody"}
	Resolve(h, c, &cards.SA{Kind: "DB", API: "Repeat", Params: map[string]string{
		"RepeatSubAbility": "Body", "RepeatOptional": "True",
	}})
	if !h.repeatBodyCalled {
		t.Fatal("RepeatOptional body suspension did not register a continuation")
	}
	if h.repeatBodyNext != 1 {
		t.Fatalf("RepeatOptional cursor = %d, want 1", h.repeatBodyNext)
	}
}

// TestRepeatOptionalResumeStatesAreDistinct pins the two resume states the
// body-suspension bug conflated:
//
//   - a completed election answered yes (AfterBody false) runs the body at
//     Next directly, and the election it then poses concerns Next+1;
//   - a body completed after its own suspension (AfterBody true) must pose
//     the election for Next BEFORE running that iteration's body, so no body
//     iteration happens and the posed election's ResumeRepeatNext is Next.
//
// The second case is the Forbidden Ritual regression in miniature: before
// AfterBody existed the continuation re-entered with the first state and
// ran a second body iteration, which is what the rules-level test catches
// end to end.
func TestRepeatOptionalResumeStatesAreDistinct(t *testing.T) {
	bodyRuns := 0
	Register("TestRepeatOptionalStateBody", func(Host, *Ctx, *cards.SA) { bodyRuns++ })
	t.Cleanup(func() { unregister("TestRepeatOptionalStateBody") })

	repeatsa := func() *cards.SA {
		return &cards.SA{Kind: "DB", API: "Repeat", Params: map[string]string{
			"RepeatSubAbility": "Body", "RepeatOptional": "True",
		}}
	}
	newCtx := func(t *testing.T) (*fakeHost, *Ctx) {
		h, c := fixtureHost(t)
		h.askResult = true
		c.SVars = map[string]string{"Body": "DB$ TestRepeatOptionalStateBody"}
		return h, c
	}

	// State A: an answered yes begins Next directly and asks no election for
	// it; the election it poses afterwards concerns Next+1.
	t.Run("answered yes begins Next", func(t *testing.T) {
		bodyRuns = 0
		h, c := newCtx(t)
		c.RepeatResume = &RepeatContinuation{Continue: true, Next: 1}
		Resolve(h, c, repeatsa())
		if bodyRuns != 1 {
			t.Fatalf("answered-yes resume ran the body %d times, want 1", bodyRuns)
		}
		if h.askCount != 1 || h.lastAsk == nil || h.lastAsk.ResumeRepeatNext != 2 {
			t.Fatalf("answered-yes resume posed %d asks and next=%v, want one election for iteration 2",
				h.askCount, askNext(h.lastAsk))
		}
	})

	// State B: a body completed after suspension. The election for Next is
	// posed, and NO body iteration runs.
	t.Run("body completion asks before Next", func(t *testing.T) {
		bodyRuns = 0
		h, c := newCtx(t)
		c.RepeatResume = &RepeatContinuation{Continue: true, Next: 1, AfterBody: true}
		Resolve(h, c, repeatsa())
		if bodyRuns != 0 {
			t.Fatalf("body-completion resume ran the body %d times before the election, want 0", bodyRuns)
		}
		if h.askCount != 1 || h.lastAsk == nil || h.lastAsk.ResumeRepeatNext != 1 {
			t.Fatalf("body-completion resume posed %d asks and next=%v, want one election for iteration 1",
				h.askCount, askNext(h.lastAsk))
		}
	})

	// State C: the "no" answer stops without another body iteration or ask.
	t.Run("answered no stops", func(t *testing.T) {
		bodyRuns = 0
		h, c := newCtx(t)
		c.RepeatResume = &RepeatContinuation{Continue: false, Next: 1}
		Resolve(h, c, repeatsa())
		if bodyRuns != 0 || h.askCount != 0 {
			t.Fatalf("stopped resume ran the body %d times and posed %d asks, want 0/0", bodyRuns, h.askCount)
		}
	})
}

func askNext(d *decision.Decision) int32 {
	if d == nil {
		return -1
	}
	return d.ResumeRepeatNext
}
