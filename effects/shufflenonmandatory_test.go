package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// searchmay1: the two ShuffleNonMandatory$ remainders the AGENTS.md row named.
//
//   (a) the fail-to-find shape (a hidden-library search that moved nothing)
//       still owes its mandatory shuffle, and now poses the may-shuffle
//       confirm instead of shuffling silently;
//   (b) an object-target ChangeZone that moved cards INTO a library and states
//       Shuffle$ True now shuffles -- and, when the flag is also set, asks.
//
// The fixtures are synthetic SAs over a fixture board: the corpus cards the
// row's own code comment names carry the flag (Squadron Hawk, Path to Exile),
// but the effects package drives the primitive directly so each precondition
// (nothing moved / the compared zone actually differs) is asserted here and
// the mandatory object-path and fail-to-find corpus paths are pinned in
// rules. Since task spcz1 SP-parented DB subs ask their own graveyard
// targeting at resolution (changeZoneChosenTargets no longer inherits the
// parent's targets), so they DO reach this tail in live play; rules/
// searchmay-style pins cover that live path end to end.

// shuffleTailBoard builds a 2-seat board with one graveyard card owned by
// seat 0 and a resolving source object. Returns the recording+suspending
// host, the Ctx sourced at the source, and the graveyard card id.
func shuffleTailBoard(t *testing.T) (*fx42AskHost, *Ctx, state.ObjID) {
	t.Helper()
	ah := &fx42AskHost{}
	ah.g = state.NewGame(names(2))
	src := ah.g.AddObject(mkCard(t, "Name:Shuffler\nTypes:Sorcery\nOracle:x\n"), 0)
	gy := ah.g.AddObject(creature(t, "Graveyard Bear"), 0)
	gy.Zone = state.ZGraveyard
	ah.g.SetZone(state.ZGraveyard, 0, []state.ObjID{gy.ID})
	ctx := &Ctx{Source: src.ID, Controller: 0, Targets: []state.Target{{Obj: gy.ID}}, TargetsOffered: true}
	return ah, ctx, gy.ID
}

// shuffleEvents counts Shuffle events for a player in the host's log.
func shuffleEvents(h *fakeHost, p state.PlayerID) int {
	n := 0
	for _, e := range h.log {
		if e.Kind == events.Shuffle && e.Player == p {
			n++
		}
	}
	return n
}

func ctxSearchShuffleLabel(accept bool) string {
	if accept {
		return "yes"
	}
	return "no"
}

// TestObjectPathShuffleMandatoryShufflesWithoutFlag is half (b)'s silent
// branch: the same object-target move stating only Shuffle$ True (the 76
// corpus "shuffle it into their library" lines that do not carry the
// non-mandatory flag) now shuffles with no ask. Before the fix the path
// shuffled nothing.
func TestObjectPathShuffleMandatoryShufflesWithoutFlag(t *testing.T) {
	ah, ctx, gy := shuffleTailBoard(t)
	s := sa(t, "DB$ ChangeZone | Origin$ Graveyard | Destination$ Library | ValidTgts$ Card.YouOwn | Shuffle$ True")

	if o := ah.g.Obj(gy); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("precondition: graveyard card zone = %v, want graveyard", o)
	}
	effChangeZone(ah, ctx, s)

	if len(ah.asks) != 0 {
		t.Fatalf("a Shuffle$ True move with no ShuffleNonMandatory$ posed %d ask(s), want none", len(ah.asks))
	}
	if got := shuffleEvents(&ah.fakeHost, 0); got != 1 {
		t.Fatalf("shuffles after the Shuffle$ True object-path move = %d, want exactly 1: %+v", got, ah.log)
	}
	if o := ah.g.Obj(gy); o == nil || o.Zone != state.ZLibrary {
		t.Fatalf("after the move: card zone = %v, want library", o)
	}

	// Guard against over-reaching: a separate Graveyard -> Library "put it
	// on top" move with NO Shuffle$ must still leave the library unshuffled.
	ah, ctx, gy = shuffleTailBoard(t)
	s = sa(t, "DB$ ChangeZone | Origin$ Graveyard | Destination$ Library | ValidTgts$ Card.YouOwn | LibraryPosition$ 0")

	if o := ah.g.Obj(gy); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("precondition: graveyard card zone = %v, want graveyard", o)
	}
	effChangeZone(ah, ctx, s)

	if len(ah.asks) != 0 {
		t.Fatalf("a no-Shuffle$ move posed %d ask(s), want none", len(ah.asks))
	}
	if got := shuffleEvents(&ah.fakeHost, 0); got != 0 {
		t.Fatalf("a no-Shuffle$ move shuffled %d time(s), want 0", got)
	}
	if o := ah.g.Obj(gy); o == nil || o.Zone != state.ZLibrary {
		t.Fatalf("after the move: card zone = %v, want library", o)
	}
}
