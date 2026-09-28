package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestCuratorOfSunsCreationRepeatsTheDiscoverValue runs the real Curator
// trigger through resolution. Its SVar X reads TriggerCount$Amount from the
// completed Discover marker, so the follow-up marker must retain the original
// discover value rather than degrading to discover 0.
func TestCuratorOfSunsCreationRepeatsTheDiscoverValue(t *testing.T) {
	t.Parallel()
	curator := tokenReplCorpusCard(t, "Curator of Sun's Creation")
	e, cfg := tokenReplGame(t, 7015704, curator)
	curatorID := moveSeededCard(t, e, 0, curator, state.ZBattlefield)
	if o := e.G.Obj(curatorID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition Curator zone = %+v, want battlefield", o)
	}

	e.emit(events.Event{Kind: events.Discover, Player: 0, Obj: curatorID, Amount: 4})
	e.pending = nil
	addMana(t, e, 0, "")
	investigateDrain(t, e)

	got := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.Discover && ev.Player == 0 && ev.Amount == 4 {
			got++
		}
	}
	if got != 2 {
		t.Fatalf("Discover markers with amount 4 = %d, want initial and Curator repeat", got)
	}
	replayCheck(t, e, cfg)
}
