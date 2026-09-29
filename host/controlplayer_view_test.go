package host

// CR 723.4 host wiring test: projectNext must widen the projected View the
// controller receives to include the hidden information of the seats they
// control. rules' controlPlayerRedirect rewrites a controlled seat's decision
// to the controller, so by the time the host projects, d.Player IS the
// controller; projectNext reverses state.Game.ControlledBy to find the
// controlled seats and passes them as the view's alsoVisible set. Without
// that, the controller answers the controlled seat's decision blind (the
// controlled hand is a count) -- the CR 723.4 gap. A third seat's hand must
// stay hidden.

import (
	"testing"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/seat"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// TestProjectNextRevealsControlledHandToController drives a live 3-seat table
// whose seat 0 is human, parks on seat 0's first decision, folds a CR 723
// control grant (seat 1 controlled by seat 0) into the engine, and projects
// the next decision through the real projectNext. The controller reads seat
// 1's hand; seat 2's stays hidden.
func TestProjectNextRevealsControlledHandToController(t *testing.T) {
	t.Parallel()
	o := testOptions(t)
	o.Seats = func(names []string, seed uint64) []seat.Seat {
		out := make([]seat.Seat, len(names))
		out[0] = NewHumanSeat()
		for i := 1; i < len(out); i++ {
			out[i] = seat.NewBot(seed ^ uint64(i))
		}
		return out
	}
	r, err := New(o)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	cfg := TableConfig{ID: "t1", Name: "human", Seats: 3, Decks: []string{"a", "b", "c"},
		Seed: 42, Pace: 0, Spectator: view.Omniscient, Perpetual: false}
	if err := r.AddTable(cfg); err != nil {
		t.Fatal(err)
	}
	if err := r.Start("t1"); err != nil {
		t.Fatal(err)
	}
	// Park on seat 0's first decision so the match goroutine is not driving
	// the engine while we project (it is blocked in the human seat's park).
	waitPending(t, r, "t1", 1, 0)

	r.mu.RLock()
	tb := r.tables["t1"]
	r.mu.RUnlock()
	if tb.cur == nil {
		t.Fatal("no live match")
	}
	m := tb.cur
	if m.e == nil || m.e.Pending() == nil {
		t.Fatal("precondition: live match with a pending decision")
	}
	d := m.e.Pending()
	if d.Player != 0 {
		t.Fatalf("precondition: seat 0's decision pending, got player %d", d.Player)
	}
	// Precondition: seat 1 really holds cards, or the positive assertion below
	// would pass on an empty hand of the wrong shape.
	if len(m.e.G.Zone(state.ZHand, 1)) == 0 {
		t.Fatal("precondition: seat 1 has no hand to reveal")
	}
	if len(m.e.G.Zone(state.ZHand, 2)) == 0 {
		t.Fatal("precondition: seat 2 has no hand to keep hidden")
	}
	// Fold a CR 723 grant: seat 0 controls seat 1. Written directly into the
	// engine's fold maps (this is the state the ControlPlayerChange fold would
	// have produced) rather than driven through a deck, because no repo deck
	// carries a ControlPlayer card and the redirect itself is rules-tested.
	m.e.G.ControlledBy = map[state.PlayerID]state.PlayerID{1: 0}
	m.e.G.ControlArmedTurn = map[state.PlayerID]int32{1: 0}

	brd := botpolicy.NewBoard(3)
	pd := projectNext(m, m.slots, &brd)
	if pd == nil {
		t.Fatalf("projectNext built no parked data: %+v", pd)
	}
	var pv1, pv2 *view.PlayerView
	for i := range pd.v.Players {
		switch pd.v.Players[i].ID {
		case 1:
			pv1 = &pd.v.Players[i]
		case 2:
			pv2 = &pd.v.Players[i]
		}
	}
	if pv1 == nil || pv2 == nil {
		t.Fatalf("expected seats 1 and 2: %+v", pd.v.Players)
	}
	if pv1.Hand == nil {
		t.Fatal("controller's view hides the controlled seat 1's hand (CR 723.4)")
	}
	if pv2.Hand != nil {
		t.Fatal("controller's view leaked uninvolved seat 2's hand")
	}
}

// TestControlledSeatsIsSortedAndDirectional pins the host's fold reversal
// directly: the controlled subjects are returned sorted for determinism, and
// only subjects naming the viewer as controller are returned.
func TestControlledSeatsIsSortedAndDirectional(t *testing.T) {
	t.Parallel()
	g := state.NewGame([]string{"a", "b", "c", "d"})
	g.ControlledBy = map[state.PlayerID]state.PlayerID{3: 0, 1: 0, 2: 1}
	got := controlledSeats(g, 0)
	if len(got) != 2 || got[0] != 1 || got[1] != 3 {
		t.Fatalf("controlledSeats(0) = %v, want sorted [1 3]", got)
	}
	if other := controlledSeats(g, 1); len(other) != 1 || other[0] != 2 {
		t.Fatalf("controlledSeats(1) = %v, want [2]", other)
	}
	if none := controlledSeats(g, 2); len(none) != 0 {
		t.Fatalf("controlledSeats(2) = %v, want none", none)
	}
}
