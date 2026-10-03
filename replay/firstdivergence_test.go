package replay

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
)

// TestFirstDivergence pins the three outcomes of comparing two complete
// streams: a differing event, a got that runs past want (Missing), and a got
// that stops short with every event matching (Short); identical is nil.
func TestFirstDivergence(t *testing.T) {
	mk := func(texts ...string) []events.Event {
		evs := make([]events.Event, len(texts))
		for i, s := range texts {
			evs[i] = events.Event{Seq: uint64(i), Kind: events.Note, Text: s}
		}
		return evs
	}
	if d := FirstDivergence(mk("a", "b"), mk("a", "b")); d != nil {
		t.Errorf("identical streams: %v", d)
	}
	if d := FirstDivergence(mk("a", "b", "c"), mk("a", "x", "c")); d == nil || d.Seq != 1 || d.Missing || d.Short || d.Want.Text != "b" || d.Got.Text != "x" {
		t.Errorf("differing event: %+v", d)
	}
	if d := FirstDivergence(mk("a"), mk("a", "b")); d == nil || d.Seq != 1 || !d.Missing || d.Got.Text != "b" {
		t.Errorf("got runs long: %+v", d)
	}
	if d := FirstDivergence(mk("a", "b"), mk("a")); d == nil || d.Seq != 1 || !d.Short || d.Want.Text != "b" {
		t.Errorf("got stops short: %+v", d)
	}
}
