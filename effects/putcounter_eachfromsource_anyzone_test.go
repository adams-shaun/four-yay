package effects

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestPutCounterEachFromSourceNonBattlefieldRecipient copies the source's
// counters to an object that remains addressable in exile.
func TestPutCounterEachFromSourceNonBattlefieldRecipient(t *testing.T) {
	h := newHost(t, 2)
	sourceID := putCounterObject(t, h)
	source := h.g.Obj(sourceID)
	source.Counters = []state.Counter{{Kind: "P1P1", N: 2}}
	recipient := putCounterExiledObject(t, h)
	if recipient.Zone != state.ZExile {
		t.Fatalf("precondition: recipient zone = %s, want Exile", recipient.Zone)
	}
	if got := source.Counter("P1P1"); got != 2 {
		t.Fatalf("precondition: source P1P1 counters = %d, want 2", got)
	}
	if got := recipient.Counter("P1P1"); got != 0 {
		t.Fatalf("precondition: recipient P1P1 counters = %d, want 0", got)
	}

	c := &Ctx{Source: sourceID, Controller: 0,
		Targets:    []state.Target{{Obj: recipient.ID}},
		Remembered: []state.Target{{Obj: sourceID}}}
	Resolve(h, c, sa(t, "SP$ PutCounter | Defined$ Targeted | CounterType$ EachFromSource | EachFromSource$ Remembered"))

	for _, ev := range h.log {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "unimplemented API") {
			t.Fatalf("PutCounter handler did not run: %q", ev.Text)
		}
	}
	if got := recipient.Counter("P1P1"); got != 2 {
		t.Fatalf("exiled recipient P1P1 counters = %d, want copied source count 2", got)
	}
	if recipient.Zone != state.ZExile {
		t.Fatalf("recipient left exile: zone = %s", recipient.Zone)
	}
}
