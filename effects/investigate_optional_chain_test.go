package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// chainInvestigateHost records every posed ask in order and marks the host
// Suspended after each, so Resolve stops descending into the SubAbility$ walk
// exactly as rules.Engine does (the fx42AskHost shape, reused here).
type chainInvestigateHost struct {
	fakeHost
	asks      []*decision.Decision
	suspended bool
}

func (h *chainInvestigateHost) Ask(d *decision.Decision) bool {
	cp := *d
	cp.Options = append([]decision.Option(nil), d.Options...)
	h.asks = append(h.asks, &cp)
	h.suspended = true
	return true
}

func (h *chainInvestigateHost) Suspended() bool { return h.suspended }

// investigateChainBoard builds a 2-seat game whose game Tokens carry the real
// corpus Clue script (the registry effInvestigate mints from), a source
// permanent on the battlefield in seat 0's control, and a Ctx resolving for
// seat 0. The returned host records asks; ctx is the shared resolution
// context the whole chain re-enters through.
func investigateChainBoard(t *testing.T) (*chainInvestigateHost, *Ctx) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	h := &chainInvestigateHost{}
	h.g = state.NewGame(names(2))
	h.g.Tokens = reg.Tokens
	src := h.g.AddObject(creature(t, "Investigator"), 0)
	src.Zone = state.ZBattlefield
	h.g.SetZone(state.ZBattlefield, 0, []state.ObjID{src.ID})
	return h, &Ctx{Source: src.ID, Controller: 0}
}

// clueCountOn counts Clue tokens in seat p's control by their token face
// name, the identity effInvestigate's TokenCreate mints.
func clueCountOn(h *chainInvestigateHost, p state.PlayerID) int {
	n := 0
	for _, id := range h.g.Zone(state.ZBattlefield, p) {
		o := h.g.Obj(id)
		if o != nil && o.Face() != nil && o.Face().Name == "Clue Token" {
			n++
		}
	}
	return n
}

// investigateMarkers counts events.Investigate records for any seat.
func investigateMarkers(h *chainInvestigateHost) int {
	n := 0
	for _, ev := range h.log {
		if ev.Kind == events.Investigate {
			n++
		}
	}
	return n
}
