package rules

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// ChangeZone.ShuffleNonMandatory$ (searchmay1): the may-shuffle confirm a
// hidden-library search offers its searcher after the fetch lands, instead of
// the unconditional CR 701.23d shuffle. Every card here is the real compiled
// corpus script loaded through the gitignored corpus registry -- no Forge
// script text is committed.

// mayShuffleConfirm asserts the pending decision IS the search's may-shuffle
// confirm and returns the yes and no option indexes.
func mayShuffleConfirm(t *testing.T, e *Engine, who state.PlayerID) (int, int) {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "search_mayshuffle" || d.Player != who {
		t.Fatalf("pending = %+v, want the may-shuffle confirm for seat %d", d, who)
	}
	if d.Prompt != "Shuffle your library?" || len(d.Options) != 2 ||
		d.Options[0].Kind != "yes" || d.Options[1].Kind != "no" {
		t.Fatalf("confirm shape = %q options %+v, want \"Shuffle your library?\" with a yes then a no", d.Prompt, d.Options)
	}
	return d.Options[0].Index, d.Options[1].Index
}

// TestSearchMayShuffleDeclinedKeepsOrder is the declined branch: the searcher
// keeps the library order the search's option list (offered in library order)
// just taught them -- no Shuffle event, the library exactly what it was minus
// the taken card.
func TestSearchMayShuffleDeclinedKeepsOrder(t *testing.T) {
	t.Parallel()
	e, cfg, id := hawkSeats(t, 1, 2)
	d := hawkCastAndAccept(t, e, id)
	if d.Min != 0 || d.Max != 2 || len(d.Options) != 2 {
		t.Fatalf("Min/Max/options = %d/%d/%d, want 0/2/2", d.Min, d.Max, len(d.Options))
	}
	before := append([]state.ObjID(nil), e.G.Zone(state.ZLibrary, 0)...)
	start := len(e.L.Events)
	taken := d.Options[0].Obj
	submitChoices(t, e, d.Options[0].Index) // take one hawk
	yes, no := mayShuffleConfirm(t, e, 0)
	_ = yes
	submitChoices(t, e, no) // decline — keep the order

	want := make([]state.ObjID, 0, len(before)-1)
	for _, oid := range before {
		if oid != taken {
			want = append(want, oid)
		}
	}
	if got := e.G.Zone(state.ZLibrary, 0); !slices.Equal(got, want) {
		t.Fatalf("library after a declined shuffle = %v, want the pre-search order %v minus the taken %d", got, before, taken)
	}
	for _, ev := range e.L.Events[start:] {
		if ev.Kind == events.Shuffle && ev.Player == 0 {
			t.Fatalf("a declined may-shuffle emitted %v", e.L.Events[start:])
		}
	}
	if n := hawkIn(e, state.ZHand); n != 1 {
		t.Fatalf("hand hawks after the fetch = %d, want the taken one", n)
	}
	replayCheck(t, e, cfg)
}

// TestSearchMayShuffleAcceptedShuffles is the accepted branch: exactly one
// Shuffle event follows the moves, and the library still holds what the
// search left there.
func TestSearchMayShuffleAcceptedShuffles(t *testing.T) {
	t.Parallel()
	e, cfg, id := hawkSeats(t, 1, 2)
	d := hawkCastAndAccept(t, e, id)
	if got := hawkIn(e, state.ZLibrary); got != 2 {
		t.Fatalf("library hawks before the search = %d, want 2", got)
	}
	submitChoices(t, e, d.Options[0].Index) // take one hawk
	yes, _ := mayShuffleConfirm(t, e, 0)
	start := len(e.L.Events)
	submitChoices(t, e, yes) // accept — shuffle

	shuffles := 0
	for _, ev := range e.L.Events[start:] {
		if ev.Kind == events.Shuffle && ev.Player == 0 {
			shuffles++
		}
	}
	if shuffles != 1 {
		t.Fatalf("seat-0 shuffles after the accepted confirm = %d, want 1: %+v", shuffles, e.L.Events[start:])
	}
	if n := hawkIn(e, state.ZLibrary); n != 1 {
		t.Fatalf("library hawks after the shuffle = %d, want the untaken one", n)
	}
	if n := hawkIn(e, state.ZHand); n != 1 {
		t.Fatalf("hand hawks after the shuffle = %d, want the taken one", n)
	}
	replayCheck(t, e, cfg)
}

// TestBoggartHarbingerMayShufflePlacement pins the tail order on the
// LibraryPosition$ shape: Boggart Harbinger's search puts the found Goblin on
// TOP of the library, and both branches leave it there -- declined, with the
// rest of the library in the order the searcher knew; accepted, with the rest
// shuffled underneath. The moved list rides the confirm (Decision.ResumeMoved
// -> Ctx.SearchShuffleMoved), which is what lets the re-entry place at all.
func TestBoggartHarbingerMayShufflePlacement(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		accept bool
	}{
		{"declined", false},
		{"accepted", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reg := searchTestRegistry(t)
			e, cfg := searchEngine(t, reg, "Boggart Harbinger", "Goblin Piker")
			bh := searchMoveByName(t, e, "Boggart Harbinger", state.ZHand)
			var goblin state.ObjID
			rest := make([]state.ObjID, 0, 16)
			for _, id := range e.G.Zone(state.ZLibrary, 0) {
				o := e.G.Obj(id)
				if o == nil || o.Face() == nil {
					continue
				}
				if o.Face().Name == "Goblin Piker" && goblin == 0 {
					goblin = id
					continue
				}
				rest = append(rest, id)
			}
			if goblin == 0 {
				t.Fatal("the seeded Goblin Piker is not in the library")
			}
			// Plant the Goblin on top (a logged LibraryOrder, the same event
			// the engine's placements emit, so the replay still verifies).
			order := append([]state.ObjID{goblin}, rest...)
			e.emit(events.Event{Kind: events.LibraryOrder, Player: 0, IDs: order, Secret: true})

			e.emit(events.Event{Kind: events.MoveZone, Obj: bh, From: state.ZHand, To: state.ZBattlefield})
			e.Advance()
			d := drainUntilAsk(t, e, 30)
			if d == nil || d.Kind != decision.KTriggerOptional {
				t.Fatalf("Boggart's optional-trigger ask missing: %+v", d)
			}
			yesIdx := -1
			for _, o := range d.Options {
				if o.Kind == "yes" {
					yesIdx = o.Index
				}
			}
			submitChoices(t, e, yesIdx)
			d = drainUntilAsk(t, e, 30)
			if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "search" || len(d.Options) != 1 || d.Options[0].Obj != goblin {
				t.Fatalf("search pick = %+v, want the one Goblin Piker (%d)", d, goblin)
			}
			submitChoices(t, e, d.Options[0].Index)
			yes, no := mayShuffleConfirm(t, e, 0)
			if tc.accept {
				start := len(e.L.Events)
				submitChoices(t, e, yes)
				shuffles := 0
				for _, ev := range e.L.Events[start:] {
					if ev.Kind == events.Shuffle && ev.Player == 0 {
						shuffles++
					}
				}
				if shuffles != 1 {
					t.Fatalf("shuffles after the accepted confirm = %d, want 1", shuffles)
				}
			} else {
				submitChoices(t, e, no)
			}
			lib := e.G.Zone(state.ZLibrary, 0)
			if len(lib) == 0 || lib[0] != goblin {
				t.Fatalf("library top after the %s confirm = %v, want the Goblin Piker %d on top", tc.name, lib, goblin)
			}
			if len(lib) != len(rest)+1 {
				t.Fatalf("library size %d, want %d (the Goblin plus the rest)", len(lib), len(rest)+1)
			}
			if !tc.accept {
				if !slices.Equal(lib[1:], rest) {
					t.Fatalf("the rest of the library after a declined shuffle = %v, want the known order %v", lib[1:], rest)
				}
			}
			replayCheck(t, e, cfg)
		})
	}
}
