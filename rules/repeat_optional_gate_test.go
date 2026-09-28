package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestRepeatOptionalSuspendedBodyChecksFalseGateBeforeElection covers the
// continuation-only branch: the real sacrifice body asks, then makes its
// Remembered$Amount gate false. Once that body answer resumes, Repeat must
// stop rather than offer the optional election or run a second sacrifice.
func TestRepeatOptionalSuspendedBodyChecksFalseGateBeforeElection(t *testing.T) {
	t.Parallel()
	const repeat = "Name:Gated Repeat\nManaCost:R\nTypes:Sorcery\n" +
		"A:SP$ Repeat | RepeatSubAbility$ Body | RepeatOptional$ True | RepeatCheckSVar$ Gate | RepeatSVarCompare$ GE3\n" +
		"SVar:Gate:Remembered$Amount\n" +
		"SVar:Body:DB$ Sacrifice | SacValid$ Permanent.!token | RememberSacrificed$ True\n" +
		"Oracle:x\n"
	const fodder = "Name:Gate Fodder\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"

	e, cfg, id := newFixtureDeck(t, 6301, repeat, fodder, fodder)
	parked := 0
	for i := range e.G.Objs {
		o := &e.G.Objs[i]
		if o.Owner != 0 || o.Face() == nil || o.Face().Name != "Gate Fodder" {
			continue
		}
		if o.Zone != state.ZHand && o.Zone != state.ZLibrary {
			t.Fatalf("test precondition: Gate Fodder zone = %s, want hand or library", o.Zone)
		}
		e.emit(events.Event{Kind: events.MoveZone, Obj: o.ID, From: o.Zone, To: state.ZBattlefield})
		parked++
	}
	if parked != 2 {
		t.Fatalf("test precondition: parked %d Gate Fodders, want 2", parked)
	}
	addMana(t, e, 0, "R")
	e.pending = nil
	e.priorityRound()

	d := castFixture(t, e, id, -1)
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "sacrifice" {
		t.Fatalf("expected the body's sacrifice ask, got %+v", d)
	}
	if got := countSacrifices(e); got != 0 {
		t.Fatalf("test precondition: sacrifices before answering body = %d, want 0", got)
	}
	submitChoices(t, e, d.Options[0].Index)

	// The answered body remembered one sacrifice, so Gate's GE3 comparison is
	// false. Its continuation must resolve the spell without a repeat election.
	if got := countSacrifices(e); got != 1 {
		t.Fatalf("body executions = %d sacrifices, want 1", got)
	}
	if d = e.Pending(); d != nil && d.ResumeKind == "repeat_optional" {
		t.Fatalf("false gate offered repeat_optional election: %+v", d)
	}
	if o := e.G.Obj(id); o != nil && o.Zone == state.ZStack {
		t.Fatalf("false gate left Repeat spell on stack: %+v", o)
	}
	replayCheck(t, e, cfg)
}
