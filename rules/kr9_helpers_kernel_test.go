package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
)

// kr9Drain passes priority and answers every other decision with answer
// (kr9FirstAnswer when nil) until the stack is empty with no queued
// triggers, the game ends, or the budget runs out (a fatal).
func kr9Drain(t *testing.T, e *Engine, limit int, answer func(d *decision.Decision) []int) {
	t.Helper()
	if answer == nil {
		answer = kr9FirstAnswer
	}
	for i := 0; i < limit; i++ {
		if e.G.Over {
			return
		}
		d := e.Pending()
		if d == nil {
			if len(e.G.Stack) == 0 && len(e.pendingTriggers) == 0 {
				return
			}
			e.priorityRound()
			continue
		}
		if d.Kind == decision.KPriority {
			if len(e.G.Stack) == 0 && len(e.pendingTriggers) == 0 {
				return
			}
			submitChoices(t, e, tapePassIndex(d))
			continue
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: answer(d)}); err != nil {
			t.Fatalf("submit %s/%s %v: %v", d.Kind, d.ResumeKind, answer(d), err)
		}
	}
	t.Fatalf("kr9Drain did not settle within %d steps", limit)
}

// kr9FirstAnswer takes the leading max(Min,1) options (none when the
// decision offers none), and the full offered order for an arrange whose
// Min covers every option.
func kr9FirstAnswer(d *decision.Decision) []int {
	n := d.Min
	if n < 1 {
		n = 1
	}
	if n > len(d.Options) {
		n = len(d.Options)
	}
	out := make([]int, 0, n)
	for j := 0; j < n; j++ {
		out = append(out, d.Options[j].Index)
	}
	return out
}

// kr9Probe runs f as a kernel probe with no stale decision pending: a
// fixture that drives the engine's internals directly (an entry emit, a
// resolveTop) at a seat's priority snapshot would otherwise leave that
// snapshot pending, and the kernel serves no ask behind a pending decision.
// The probe's ask (if any) is the pending decision afterwards.
func kr9Probe(e *Engine, f func()) {
	e.pending = nil
	e.probe(f)
}

// kr9ResolveTop resolves the top of the stack as a kernel probe.
func kr9ResolveTop(e *Engine) { kr9Probe(e, e.resolveTop) }

// kr9Settle re-asks priority (which first stacks queued triggers, CR 117.5)
// when a probe's answering Submit left nothing pending: a probe re-runs only
// its own step, so the next priority round is the fixture's to start.
func kr9Settle(e *Engine) {
	if e.Pending() == nil && !e.G.Over {
		e.priorityRound()
	}
}
