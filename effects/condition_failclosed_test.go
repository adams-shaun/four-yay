package effects

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/events"
)

// TestUnmodelledConditionFailsClosedWithNote pins the fail-closed contract for
// an unmodelled Condition* gate: a sub carrying a shape conditionMet cannot
// evaluate (here ConditionManaSpent$, which is not read at all) must NOT run
// its body, and the resolve walk must emit a replay-visible Note naming the
// shape, instead of running the rider unconditionally.
func TestUnmodelledConditionFailsClosedWithNote(t *testing.T) {
	// Precondition: the shape really is classified unmodelled, and the SA
	// really carries it (otherwise the body would run for an unrelated reason).
	s := sa(t, "DB$ GainLife | LifeAmount$ 2 | ConditionManaSpent$ R")
	if detail, bad := UnmodelledCondition(s); !bad || !strings.Contains(detail, "ConditionManaSpent") {
		t.Fatalf("precondition: UnmodelledCondition = (%q, %v), want a ConditionManaSpent classification", detail, bad)
	}
	if _, ok := s.Params["ConditionManaSpent"]; !ok {
		t.Fatalf("precondition: the parsed SA must carry ConditionManaSpent$")
	}

	h := newHost(t, 2)
	Resolve(h, &Ctx{Controller: 0}, s)

	var note string
	bodyRan := false
	for _, ev := range h.log {
		switch ev.Kind {
		case events.Note:
			if strings.Contains(ev.Text, "unmodelled condition") {
				note = ev.Text
			}
		case events.LifeChange:
			bodyRan = true
		}
	}
	if note == "" {
		t.Fatalf("no unmodelled-condition Note emitted; log=%+v", h.log)
	}
	if !strings.Contains(note, "ConditionManaSpent") {
		t.Fatalf("Note %q does not name the unmodelled shape", note)
	}
	if bodyRan {
		t.Fatalf("the gated body ran despite the unmodelled condition; log=%+v", h.log)
	}
}
