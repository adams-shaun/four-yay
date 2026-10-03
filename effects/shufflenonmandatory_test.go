package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
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

// TestObjectPathShuffleNonMandatoryAsks is half (b): an object-target
// Graveyard -> Library move with Shuffle$ True | ShuffleNonMandatory$ True
// (Cathartic Parting, Devious Cover-Up, Covetous Castaway, Put Away) moves
// the card, poses Forge's may-shuffle confirm, and shuffles ONLY if the
// searcher says yes when this SA receives its own graveyard target. The
// named SP-parented DB carriers DO receive their own target since task spcz1
// (rules/ pins the live path); this fixture exercises the tail machinery
// itself, decoupled from any parent/sub targeting.
func TestObjectPathShuffleNonMandatoryAsks(t *testing.T) {
	for _, accept := range []bool{true, false} {
		name := "decline keeps order"
		if accept {
			name = "accept shuffles"
		}
		t.Run(name, func(t *testing.T) {
			ah, ctx, gy := shuffleTailBoard(t)
			s := sa(t, "DB$ ChangeZone | Origin$ Graveyard | Destination$ Library | ValidTgts$ Card.YouOwn | Shuffle$ True | ShuffleNonMandatory$ True")

			// Precondition: the card is in the zone the Origin$ precondition
			// reads, so the move is a real move (not a skipped target).
			if o := ah.g.Obj(gy); o == nil || o.Zone != state.ZGraveyard {
				t.Fatalf("precondition: graveyard card zone = %v, want graveyard", o)
			}

			effChangeZone(ah, ctx, s)

			// The move landed before the ask (the confirm protects the order
			// the moved card is now part of).
			if o := ah.g.Obj(gy); o == nil || o.Zone != state.ZLibrary {
				t.Fatalf("after the move: card zone = %v, want library", o)
			}
			if len(ah.asks) != 1 {
				t.Fatalf("object-path shuffle posed %d decisions, want the one may-shuffle confirm", len(ah.asks))
			}
			d := ah.asks[0]
			if d.Kind != decision.KChoose || d.ResumeKind != "search_mayshuffle" || d.Prompt != "Shuffle your library?" {
				t.Fatalf("ask = %+v, want the may-shuffle confirm", d)
			}
			if len(d.ResumeMoved) != 1 || d.ResumeMoved[0] != gy {
				t.Fatalf("confirm ResumeMoved = %v, want the moved card %d", d.ResumeMoved, gy)
			}
			if got := shuffleEvents(&ah.fakeHost, 0); got != 0 {
				t.Fatalf("a pending confirm already shuffled %d time(s)", got)
			}

			// Engine resume: the answered confirm re-enters effChangeZone,
			// which must consume the answer and shuffle only on "yes".
			ah.suspended = false
			if accept {
				ctx.SearchShuffle = "yes"
			} else {
				ctx.SearchShuffle = "no"
			}
			ctx.SearchShuffleMoved = append([]state.ObjID(nil), d.ResumeMoved...)
			effChangeZone(ah, ctx, s)

			want := 0
			if accept {
				want = 1
			}
			if got := shuffleEvents(&ah.fakeHost, 0); got != want {
				t.Fatalf("shuffles after a %q answer = %d, want %d: %+v", ctxSearchShuffleLabel(accept), got, want, ah.log)
			}
			// The card stayed in the library on both branches (the re-entry
			// must not move it a second time).
			if o := ah.g.Obj(gy); o == nil || o.Zone != state.ZLibrary {
				t.Fatalf("after the resume: card zone = %v, want library", o)
			}
		})
	}
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

// TestSearchShuffleTailFailToFindAsks is half (a): a hidden-library search
// that moved NOTHING still owes its mandatory shuffle (CR 701.23b), and now
// poses the may-shuffle confirm instead of shuffling silently. Before the fix
// the fail-to-find shape took the mandatory shuffle with no ask.
func TestSearchShuffleTailFailToFindAsks(t *testing.T) {
	for _, accept := range []bool{true, false} {
		name := "decline keeps order"
		if accept {
			name = "accept shuffles"
		}
		t.Run(name, func(t *testing.T) {
			ah := &fx42AskHost{}
			ah.g = state.NewGame(names(2))
			src := ah.g.AddObject(mkCard(t, "Name:Searcher\nTypes:Sorcery\nOracle:x\n"), 0)
			ctx := &Ctx{Source: src.ID, Controller: 0}
			s := sa(t, "DB$ ChangeZone | Origin$ Library | Destination$ Hand | ChangeType$ Card.namedNothing | ShuffleNonMandatory$ True")

			// The fail-to-find shape is real: no card can be moved from
			// this empty library, but the search still owes a shuffle.
			if got := len(ah.g.Zone(state.ZLibrary, 0)); got != 0 {
				t.Fatalf("precondition: library holds %d cards, want 0", got)
			}
			if searchShuffleTail(ah, ctx, s, ChangeZoneOf(s), 0, nil, state.ZHand) != true {
				t.Fatalf("fail-to-find tail returned false, want the suspended confirm")
			}
			if len(ah.asks) != 1 {
				t.Fatalf("fail-to-find posed %d decisions, want the one may-shuffle confirm", len(ah.asks))
			}
			d := ah.asks[0]
			if d.Kind != decision.KChoose || d.ResumeKind != "search_mayshuffle" || d.Prompt != "Shuffle your library?" {
				t.Fatalf("ask = %+v, want the may-shuffle confirm", d)
			}
			if len(d.ResumeMoved) != 0 {
				t.Fatalf("fail-to-find confirm ResumeMoved = %v, want empty (nothing moved)", d.ResumeMoved)
			}
			if got := shuffleEvents(&ah.fakeHost, 0); got != 0 {
				t.Fatalf("a pending fail-to-find confirm already shuffled %d time(s)", got)
			}

			ah.suspended = false
			if accept {
				ctx.SearchShuffle = "yes"
			} else {
				ctx.SearchShuffle = "no"
			}
			ctx.SearchShuffleMoved = nil
			searchShuffleTail(ah, ctx, s, ChangeZoneOf(s), 0, nil, state.ZHand)

			want := 0
			if accept {
				want = 1
			}
			if got := shuffleEvents(&ah.fakeHost, 0); got != want {
				t.Fatalf("fail-to-find shuffles after a %q answer = %d, want %d: %+v", ctxSearchShuffleLabel(accept), got, want, ah.log)
			}
		})
	}
}
