package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// latestCastOriginByScan is the reference the field read replaced: the
// object's LATEST PutOnStack event's (From, Player), found by walking the
// whole log backwards. It is deliberately kept here as the oracle the
// event-folded state.Object field must reproduce byte-for-byte.
func latestCastOriginByScan(e *Engine, obj state.ObjID) (state.Zone, state.PlayerID, bool) {
	for i := len(e.L.Events) - 1; i >= 0; i-- {
		ev := e.L.Events[i]
		if ev.Kind == events.PutOnStack && ev.Obj == obj {
			return ev.From, ev.Player, true
		}
	}
	return 0, 0, false
}

// TestLatestCastOriginMatchesLogScan plays a real acceptance game and, at
// every PlayAcceptance checkpoint, asserts that the event-folded
// state.Object field latestCastOrigin reads equals the reverse log scan it
// replaced for EVERY object. This is the invariant that makes the field a
// safe replacement: a log-only replay and a clone both derive the same pair.
func TestLatestCastOriginMatchesLogScan(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("long")
	}
	reg := testutil.CorpusRegistry(t)
	names, decks, err := testutil.AcceptanceDecks(reg, 2)
	if err != nil {
		t.Fatalf("%v", err)
	}
	cfg := AcceptanceConfig(reg, names, decks)
	castSeen, checked := false, 0
	_, _, err = PlayAcceptance(cfg, acceptanceTestBot, func(e *Engine, n int) {
		for i := range e.G.Objs {
			o := &e.G.Objs[i]
			gotFrom, gotBy, gotOK := e.latestCastOrigin(o.ID)
			wantFrom, wantBy, wantOK := latestCastOriginByScan(e, o.ID)
			if gotOK != wantOK || (gotOK && (gotFrom != wantFrom || gotBy != wantBy)) {
				t.Fatalf("checkpoint %d: obj %d field=(%v,%v,%v) scan=(%v,%v,%v)",
					n, o.ID, gotFrom, gotBy, gotOK, wantFrom, wantBy, wantOK)
			}
			checked++
			if gotOK {
				castSeen = true
			}
		}
	})
	if err != nil {
		t.Fatalf("acceptance game: %v", err)
	}
	// Precondition: the game must actually have cast something and have had
	// objects to check, or every comparison above was vacuously equal.
	if !castSeen {
		t.Fatal("no object ever carried a recorded cast origin: the comparison was vacuous")
	}
	if checked == 0 {
		t.Fatal("no object was checked across the whole game")
	}
	t.Logf("checked %d object/checkpoint pairs, at least one carrying a cast origin", checked)
}

// TestLatestCastOriginSurvivesReversal folds a PutOnStack and then the CR
// 733.1 logged reverse move straight through events.Apply and asserts the
// recorded origin is still the PutOnStack's: the reverse scan the field
// replaced still finds that PutOnStack in the log after a reversal, so
// clearing the field there would diverge from it.
func TestLatestCastOriginSurvivesReversal(t *testing.T) {
	t.Parallel()
	g := state.NewGame([]string{"a", "b"})
	o := g.AddObject(nil, 0)
	if o == nil || o.ID == 0 {
		t.Fatal("no object minted")
	}
	events.Apply(g, events.Event{Kind: events.PutOnStack, Obj: o.ID, Player: 1, From: state.ZHand, To: state.ZStack})
	if !o.HasLatestCast || o.LatestCastFrom != state.ZHand || o.LatestCastBy != 1 {
		t.Fatalf("after PutOnStack: has=%v from=%v by=%v", o.HasLatestCast, o.LatestCastFrom, o.LatestCastBy)
	}
	events.Apply(g, events.Event{Kind: events.MoveZone, Obj: o.ID, From: state.ZStack, To: state.ZHand, Text: "reversed"})
	if !o.HasLatestCast || o.LatestCastFrom != state.ZHand || o.LatestCastBy != 1 {
		t.Fatalf("after reversed move: has=%v from=%v by=%v", o.HasLatestCast, o.LatestCastFrom, o.LatestCastBy)
	}
	// A never-cast object must read false: the object's precondition for the
	// positive read is that a PutOnStack actually folded.
	never := g.AddObject(nil, 0)
	if never.HasLatestCast {
		t.Fatal("a never-cast object must not carry a cast origin")
	}
}
