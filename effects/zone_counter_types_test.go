package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func TestChangeZoneCommaCounterKindsEmitIndividually(t *testing.T) {
	h := &fakeHost{g: state.NewGame([]string{"P"})}
	emitChangeZoneCounters(h, 7, "Hexproof,Indestructible", 1)
	if len(h.log) != 2 {
		t.Fatalf("counter events = %d, want 2", len(h.log))
	}
	for i, want := range []string{"Hexproof", "Indestructible"} {
		ev := h.log[i]
		if ev.Kind != events.CounterChange || ev.Obj != 7 || ev.Counter != want || ev.Amount != 1 {
			t.Errorf("event %d = %+v, want CounterChange %s x1 on object 7", i, ev, want)
		}
	}
}
