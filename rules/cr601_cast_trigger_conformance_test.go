package rules

// Reference: Magic: The Gathering Comprehensive Rules, 2026-08-07 revision.
// CR 601.2i (4753-4756): "Any abilities that trigger when a spell is cast
// or put onto the stack trigger at this time." That time is AFTER 601.2a-h,
// not the provisional stack entry before its target is even announced.

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// Unlike the CR 733 illegal-cast probe, this cast is legal and succeeds.
// A target has not become chosen at the first boundary, so there can be NO
// cast trigger yet, even in a private queue. After the answer the Pyromancer
// trigger must drain exactly once and the caster must retain priority.
func TestCR601CastTriggerWaitsForCompletedProposal(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := crAbortEngine(t, reg, "ur-delver")
	pyro := crAbortPyromancer(t, e)
	id := crAbortMove(t, e, 0, "Lightning Bolt", state.ZHand)
	sa := e.G.Obj(id).Face().SpellAbility()
	if sa == nil || sa.API != "DealDamage" || sa.Params["ValidTgts"] != "Any" {
		t.Fatalf("CR 601.2i Lightning Bolt seq %d: compiled fixture changed: %+v", len(e.L.Events), sa)
	}
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "R", Amount: 1})
	e.askPriority(0)
	checked, start := 0, len(e.L.Events)
	checked++
	crAbortAnswer(t, e, "Lightning Bolt", crAbortOption(t, e, "Lightning Bolt", "cast", id))
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget || d.Source != id {
		t.Fatalf("CR 601.2c/i Lightning Bolt seq %d: missing unanswered target decision: %+v", start, d)
	}
	for _, ev := range e.L.Events[start:] {
		if ev.Kind == events.TargetsChosen && ev.Obj == id {
			t.Fatalf("CR 601.2c/i Lightning Bolt seq %d: target already chosen at %d", start, ev.Seq)
		}
	}
	if len(e.pendingTriggers) != 0 {
		t.Errorf("CR 601.2i Lightning Bolt target seq %d: Young Pyromancer already queued (%d triggers) before the target answer", d.Seq, len(e.pendingTriggers))
	}
	for _, ev := range e.L.Events[start:] {
		if ev.Kind == events.TriggerPush && ev.Obj == pyro {
			t.Errorf("CR 601.2i Lightning Bolt seq %d: premature Pyromancer push at %d", start, ev.Seq)
		}
	}
	idx := -1
	for _, opt := range d.Options {
		if opt.Kind == "player" && opt.Player == 1 {
			idx = opt.Index
		}
	}
	if idx < 0 {
		t.Fatalf("CR 601.2c/i Lightning Bolt seq %d: living opponent not offered", d.Seq)
	}
	crAbortAnswer(t, e, "Lightning Bolt", idx)
	pushes := 0
	for _, ev := range e.L.Events[start:] {
		if ev.Kind == events.TriggerPush && ev.Obj == pyro {
			pushes++
		}
	}
	if pushes != 1 || len(e.pendingTriggers) != 0 || e.G.Priority != 0 || e.Pending() == nil || e.Pending().Kind != decision.KPriority || e.Pending().Player != 0 {
		t.Errorf("CR 601.2i Lightning Bolt seq %d: completed cast must drain one Pyromancer trigger and retain caster priority; pushes=%d queue=%d priority=%d next=%+v", start, pushes, len(e.pendingTriggers), e.G.Priority, e.Pending())
	}
	t.Logf("MEASURED CR 601.2i Lightning Bolt seq %d: after target answer pushes=%d queue=%d priority=%d", start, pushes, len(e.pendingTriggers), e.G.Priority)
	if checked == 0 {
		t.Fatal("CR 601.2i Lightning Bolt seq 0: examined zero proposals")
	}
}
