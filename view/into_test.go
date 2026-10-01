// This file is package view_test for the same reason visibility_test.go is:
// it drives real games with seat.Bot, and seat imports view.
package view_test

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/seat"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// projCase is one projection ProjectInto must reproduce: a viewer, a
// visibility and an optional CR 723.4 widening set.
type projCase struct {
	viewer state.PlayerID
	vis    view.Visibility
	also   []state.PlayerID
}

func projCases(seats int) []projCase {
	var cs []projCase
	viewers := []state.PlayerID{view.NoSeat}
	for p := 0; p < seats; p++ {
		viewers = append(viewers, state.PlayerID(p))
	}
	for _, v := range viewers {
		for _, vis := range []view.Visibility{view.Seat, view.Public, view.Omniscient} {
			cs = append(cs, projCase{viewer: v, vis: vis})
		}
	}
	// The widening set, for two real viewers (the next seat is visible to
	// each), and a Visibility value no switch names (the Seat default).
	cs = append(cs,
		projCase{viewer: 0, vis: view.Seat, also: []state.PlayerID{1}},
		projCase{viewer: 2, vis: view.Seat, also: []state.PlayerID{3, 0}},
		projCase{viewer: 1, vis: view.Visibility(9)},
	)
	return cs
}

// equalProjection fails t unless got is exactly want: reflect.DeepEqual (nil
// and empty lists and maps distinguished) and byte-identical JSON.
func equalProjection(t *testing.T, where string, got, want view.View) {
	t.Helper()
	if reflect.DeepEqual(got, want) {
		return
	}
	gj, _ := json.Marshal(got)
	wj, _ := json.Marshal(want)
	if string(gj) != string(wj) {
		t.Fatalf("%s: ProjectInto differs from the fresh projection\n got: %s\nwant: %s", where, gj, wj)
	}
	t.Fatalf("%s: ProjectInto is JSON-equal but not DeepEqual to the fresh projection (a nil/empty or pointer difference)\n got: %#v\nwant: %#v", where, got, want)
}

// TestProjectIntoMatchesProjectAcrossWholeGames is ProjectInto's semantic
// pin: over whole real games, at every decision, for every seat, the
// spectator, every visibility and the widening set, a View refilled in place
// is exactly the fresh projection. Two reuse disciplines run side by side:
// one dst per case (the per-worker shape: same viewer, consecutive states)
// and ONE dst cycled through every case and every game (the adversarial
// shape: each refill follows a different viewer, visibility, state and even
// game, so any stale slot, map key, pointer or nil-ness would surface).
func TestProjectIntoMatchesProjectAcrossWholeGames(t *testing.T) {
	const seats, maxDecisions = 4, 400
	names, decks := testutil.SampleDecks(t, seats)
	cases := projCases(seats)
	perCase := make([]view.View, len(cases))
	var shared view.View
	checked := 0
	for _, seed := range []uint64{1, 7, 42} {
		e := rules.New(rules.Config{Seed: seed, Names: names, Decks: decks})
		e.Advance()
		b := seat.NewBot(seed)
		check := func() {
			d := e.Pending()
			for k, c := range cases {
				want := view.ProjectForControlledFor(e.G, e, c.viewer, c.vis, c.also, d)
				view.ProjectForControlledInto(&perCase[k], e.G, e, c.viewer, c.vis, c.also, d)
				equalProjection(t, "per-case dst", perCase[k], want)
				view.ProjectForControlledInto(&shared, e.G, e, c.viewer, c.vis, c.also, d)
				equalProjection(t, "shared dst", shared, want)
				checked++
			}
			// The two thin entry points are the same projection.
			if d != nil {
				view.ProjectInto(&shared, e.G, e, d.Player, d)
				equalProjection(t, "ProjectInto", shared, view.Project(e.G, e, d.Player, d))
				view.ProjectForInto(&shared, e.G, e, d.Player, view.Omniscient, d)
				equalProjection(t, "ProjectForInto", shared, view.ProjectFor(e.G, e, d.Player, view.Omniscient, d))
			}
		}
		for i := 0; i < maxDecisions && !e.G.Over && e.Pending() != nil; i++ {
			check()
			d := e.Pending()
			in, err := b.Decide(context.Background(), view.Project(e.G, e, d.Player, d), *d)
			if err != nil {
				t.Fatal(err)
			}
			if err := e.Submit(in); err != nil {
				t.Fatalf("seed %d intent %d: %v", seed, i, err)
			}
		}
		check() // the final (possibly terminal) state
	}
	if checked < 1000 {
		t.Fatalf("only %d projections compared; the games ended too early to pin anything", checked)
	}
}

// TestProjectIntoNilGame pins the degenerate inputs on a reused dst: a nil
// game leaves only the viewer and visibility (and, for Omniscient, the
// decision copy), exactly as the fresh projection does.
func TestProjectIntoNilGame(t *testing.T) {
	e := playSome(t, 3, 40)
	d := e.Pending()
	var dst view.View
	for _, vis := range []view.Visibility{view.Seat, view.Public, view.Omniscient} {
		view.ProjectForInto(&dst, e.G, e, 0, vis, d)
		view.ProjectForInto(&dst, nil, e, 0, vis, d)
		equalProjection(t, "nil game "+vis.String(), dst, view.ProjectFor(nil, e, 0, vis, d))
		view.ProjectForInto(&dst, e.G, nil, 1, vis, d)
		equalProjection(t, "nil chars "+vis.String(), dst, view.ProjectFor(e.G, nil, 1, vis, d))
	}
}
