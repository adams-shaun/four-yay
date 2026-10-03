package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// lookupCard fetches one compiled corpus card as a deck supplement.
func lookupCard(t *testing.T, reg *cards.Registry, name string) *cards.Card {
	t.Helper()
	c, ok := reg.Lookup(name)
	if !ok {
		t.Fatalf("missing corpus card %s", name)
	}
	return c
}

// drainToSuspendedAsk passes every priority decision until a non-priority,
// suspended-resolution decision (the replacement's mid-resolution ask)
// surfaces, or the bound is exhausted. It returns nil if none did.
func drainToSuspendedAsk(t *testing.T, e *Engine, bound int) *decision.Decision {
	t.Helper()
	for i := 0; i < bound; i++ {
		d := e.Pending()
		if d == nil {
			return nil
		}
		if d.Kind != decision.KPriority {
			return d
		}
		crAbortAnswer(t, e, "drain", crAbortOption(t, e, "drain", "pass", 0))
	}
	return nil
}

// seedLands moves up to n land cards from seat p's library to its hand.
func seedLands(t *testing.T, e *Engine, p state.PlayerID, n int) {
	t.Helper()
	moved := 0
	for _, id := range append([]state.ObjID{}, e.G.Zone(state.ZLibrary, p)...) {
		o := e.G.Obj(id)
		if o == nil || o.Face() == nil || !strings.Contains(strings.Join(o.Face().Types, " "), "Land") {
			continue
		}
		e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZLibrary, To: state.ZHand})
		moved++
		if moved >= n {
			return
		}
	}
	if moved < n {
		t.Fatalf("seedLands: only %d lands available in seat %d's library, want %d", moved, p, n)
	}
}
