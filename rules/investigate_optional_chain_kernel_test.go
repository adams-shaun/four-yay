package rules

// Restores effects/investigate_optional_chain_test.go on the kernel: a
// SECOND Optional$ Investigate chained in the same resolution is its own
// election, not a consumer of the first one's answer.

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func TestChainedOptionalInvestigatePosesItsOwnElection(t *testing.T) {
	t.Parallel()
	e := kr2Engine(t, 2)
	spell := kr2Put(t, e, 0, kr2Sorcery(t, "Twice Wondered",
		"A:SP$ Investigate | Optional$ True | Defined$ Opponent | SubAbility$ DBInv",
		"SVar:DBInv:DB$ Investigate | Optional$ True | Defined$ Opponent"), state.ZHand, false)
	from := len(e.L.Events)
	d := kr2Cast(t, e, 0, spell)
	for i := 0; i < 2; i++ {
		if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "investigate_optional" || d.Player != 1 {
			t.Fatalf("election %d = %+v, want the opponent's (seat 1) investigate_optional KChoose", i, d)
		}
		d = kr2Answer(t, e, d, kr2Kind(t, d, "yes"))
	}
	if d != nil {
		t.Fatalf("a third ask followed the two elections: %+v", d)
	}
	clues := 0
	for _, id := range e.G.Zone(state.ZBattlefield, 1) {
		if o := e.G.Obj(id); o.IsToken && o.Face().Name == "Clue Token" {
			clues++
		}
	}
	if clues != 2 {
		t.Fatalf("seat 1 Clues = %d, want 2 (one per accepted election)", clues)
	}
	if n := len(kr2Events(e, from, events.Investigate)); n != 2 {
		t.Fatalf("Investigate markers = %d, want 2", n)
	}
}
