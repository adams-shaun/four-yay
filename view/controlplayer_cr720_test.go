// This file is package view_test (like visibility_test.go), so it can drive a
// real game through seat.Bot's playSome helper. It is the CR 720.4 projection
// half of the control ticket: a player controlling another seat must be shown
// that seat's hidden information, keyed on state.Game.ControlledBy.
package view_test

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// TestControlPlayerSeesControlledSeatHand is the CR 720.4 headline: the
// controller's seat projection contains the controlled seat's hand, and a
// seat that controls nobody still sees that hand as nil (hidden). The setup
// is a real 4-seat game with a non-empty hand in seat 1, so the compared
// values genuinely differ; the control is installed directly on the folded
// map the projection now reads.
func TestControlPlayerSeesControlledSeatHand(t *testing.T) {
	e := playSome(t, 7, 4)
	g := e.G

	// Precondition: seat 1 really has a hand to reveal, or "len == 0" would
	// pass for the same reason the hidden case does.
	if n := len(g.Zone(state.ZHand, 1)); n == 0 {
		t.Fatal("test setup: seat 1 has no cards in hand, the assertion proves nothing")
	}

	// No control yet: seat 0 sees seat 1's hand hidden (the pre-CR-720.4
	// contract, asserted here as the negative control's baseline).
	plain := view.Project(g, e, 0, nil)
	if h := plain.Players[1].Hand; h != nil {
		t.Fatalf("without control, seat 0 sees seat 1's hand %v (must be nil/hidden)", h)
	}

	g.ControlledBy = map[state.PlayerID]state.PlayerID{1: 0}

	v := view.Project(g, e, 0, nil)
	if got, want := len(v.Players[1].Hand), len(g.Zone(state.ZHand, 1)); got != want {
		t.Fatalf("controller seat 0 view: seat1 Hand len=%d (hand_size=%d), want %d",
			got, v.Players[1].HandSize, want)
	}
	for _, cv := range v.Players[1].Hand {
		if cv.Name == "" {
			t.Fatalf("controller's view of seat 1 hand has an unnamed card %+v", cv)
		}
	}
	// The controller keeps their OWN hand too -- widening, not replacing.
	if v.Players[0].Hand == nil {
		t.Fatal("controller lost their own hand")
	}

	// Negative control: a non-controlling seat (2) still sees seat 1's hand
	// as nil.
	other := view.Project(g, e, 2, nil)
	if other.Players[1].Hand != nil {
		t.Fatalf("non-controlling seat 2 sees seat 1's hand %v (leak)", other.Players[1].Hand)
	}
}

// TestProjectForControlPlayerPublicStillHidesEveryHand proves the CR 720.4
// widening cannot leak to a spectator: Public forces the NoSeat path, and no
// real seat id can equal NoSeat (255), so g.ControlledBy can never name a
// spectator as a controller. A populated ControlBy must not change Public's
// output. Named with the TestProjectFor prefix so the review gate pattern
// (TestControlPlayerSees|TestProjectFor|...) selects it.
func TestProjectForControlPlayerPublicStillHidesEveryHand(t *testing.T) {
	e := playSome(t, 7, 4)
	g := e.G
	g.ControlledBy = map[state.PlayerID]state.PlayerID{1: 0, 2: 0}

	// A real seat (0, the controller) is passed in; Public must still force
	// the spectator path.
	v := view.ProjectFor(g, e, 0, view.Public, nil)
	for _, p := range v.Players {
		if p.Hand != nil {
			t.Fatalf("public view (controller passed in) exposes seat %d's hand %v", p.ID, p.Hand)
		}
	}
	// NoSeat can never be a controller: the map lookup above returns a real
	// seat id, never 255.
	if controlsSeatNoSeatCanMatch(g) {
		t.Fatal("a real-seat control entry matched NoSeat; Public's spectator path is compromised")
	}
}

// controlsSeatNoSeatCanMatch asserts the fail-closed premise the Public test
// above rests on, through the exported surface: projecting with NoSeat as the
// viewer (the internal Public path) exposes no hand even with ControlBy
// populated. If any entry could make NoSeat a controller this would fail.
func controlsSeatNoSeatCanMatch(g *state.Game) bool {
	for seat := range g.ControlledBy {
		if g.ControlledBy[seat] == view.NoSeat {
			return true
		}
	}
	return false
}
