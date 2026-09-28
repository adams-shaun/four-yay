package rules

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// Burning Inquiry's real corpus Discard sub-ability draws three independently
// from each remaining hand. Both the chosen object and the one-based random
// index must be in the applied discard event, not just in the engine's RNG.
func TestBurningInquiryRandomDiscardIsLoggedAndReplayable(t *testing.T) {
	t.Parallel()
	e := crResolutionEngine(t, []string{"Burning Inquiry"}, nil)
	id := crAbortMove(t, e, 0, "Burning Inquiry", state.ZHand)
	face := e.G.Obj(id).Face()
	if face == nil || face.SpellAbility() == nil || face.SpellAbility().Sub == nil ||
		face.SpellAbility().Sub.API != "Discard" || face.SpellAbility().Sub.Params["Mode"] != "Random" {
		t.Fatal("precondition: Burning Inquiry no longer resolves a random discard")
	}
	for p := range e.G.Players {
		if len(e.G.Zone(state.ZHand, state.PlayerID(p))) < 3 {
			t.Fatalf("precondition: seat %d has too few cards to distinguish a random pick", p)
		}
	}
	before := e.G.Clone()
	other := e.Clone()
	start, draws := len(e.L.Events), e.RNGDraws()
	e.resolveAbility(id, 0, nil, face.SpellAbility().Sub, face.SVars)
	other.resolveAbility(id, 0, nil, other.G.Obj(id).Face().SpellAbility().Sub, other.G.Obj(id).Face().SVars)
	if e.L.Head() != other.L.Head() || e.RNGDraws() != other.RNGDraws() {
		t.Fatal("identically seeded random discards did not reproduce the same events/RNG position")
	}
	if got := e.RNGDraws() - draws; got != 6 {
		t.Fatalf("RNG draws=%d, want one per discard across both players", got)
	}
	moved, nonFront := 0, 0
	for _, ev := range e.L.Events[start:] {
		if events.IsDiscard(ev) {
			moved++
			hand := before.Zone(state.ZHand, ev.Player)
			if ev.Amount < 1 || int(ev.Amount) > len(hand) || hand[ev.Amount-1] != ev.Obj {
				t.Fatalf("random choice index does not identify the applied discard: %+v hand=%v", ev, hand)
			}
			if ev.Amount > 1 {
				nonFront++
			}
		}
		events.Apply(before, ev)
	}
	if nonFront == 0 {
		t.Fatal("precondition: seed made every random discard the front card")
	}
	if moved != 6 {
		t.Fatalf("logged random discards=%d, want 6", moved)
	}
	for p := range e.G.Players {
		player := state.PlayerID(p)
		for _, z := range []state.Zone{state.ZHand, state.ZGraveyard} {
			if !slices.Equal(before.Zone(z, player), e.G.Zone(z, player)) {
				t.Fatalf("event-only replay seat %d zone %s diverged", p, z)
			}
		}
	}
}
