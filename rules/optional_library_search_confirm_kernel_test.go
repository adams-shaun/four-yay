package rules

// Restores effects/optional_library_search_confirm_test.go on the kernel: an
// explicit Optional$ library search asks search_confirm BEFORE the fetch list
// is consulted; a decline skips that player's search and tail, an accept
// enters the ordinary Min-0 search (which may find nothing, and whose empty
// pool still owes its may-shuffle tail), ChoiceOptional$ and a markerless
// text-may never confirm, an object-valued Defined$ fetch keeps its own
// single election, Path to Exile's basic-land search is the corpus carrier,
// and in a multi-player search a decline skips only the answering player
// while an answered player is never re-asked.

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

const kr2SearchOptional = "A:SP$ ChangeZone | Origin$ Library | Destination$ Hand | ChangeType$ Creature | ChangeNum$ 1 | Optional$ True"

// kr2SearchFetch puts two creatures on top of seat 0's library (bear above
// bird) and casts line; with noCreature the library holds only Mountains.
func kr2SearchFetch(t *testing.T, line string, noCreature bool) (*Engine, state.ObjID, []state.ObjID, *decision.Decision) {
	t.Helper()
	e := kr2Engine(t, 2)
	var ids []state.ObjID
	if !noCreature {
		bird := kr2Put(t, e, 0, kr2Src(t, "Name:Birds of Paradise\nManaCost:G\nTypes:Creature Bird\nPT:0/1\nOracle:x\n"), state.ZLibrary, true)
		bear := kr2Put(t, e, 0, kr2Src(t, kr2BearSrc), state.ZLibrary, true)
		ids = []state.ObjID{bear, bird}
	}
	spell := kr2Put(t, e, 0, kr2Sorcery(t, "Searcher", line), state.ZHand, false)
	return e, spell, ids, kr2Cast(t, e, 0, spell)
}

func TestOptionalLibrarySearchConfirm(t *testing.T) {
	t.Parallel()
	t.Run("gate then decline", func(t *testing.T) {
		t.Parallel()
		e, spell, ids, d := kr2SearchFetch(t, kr2SearchOptional, false)
		kr2RequireConfirm(t, d, "search_confirm")
		if d.Source != spell {
			t.Fatalf("confirm Source = %d, want the searching spell %d", d.Source, spell)
		}
		kr2RequireZones(t, e, ids, state.ZLibrary, "the confirmation itself")
		if d = kr2Answer(t, e, d, kr2Kind(t, d, "no")); d != nil {
			t.Fatalf("a declined confirmation posed %+v", d)
		}
		kr2RequireZones(t, e, ids, state.ZLibrary, "decline")
	})
	t.Run("accept picks one", func(t *testing.T) {
		t.Parallel()
		e, _, ids, d := kr2SearchFetch(t, kr2SearchOptional, false)
		d = kr2Want(t, kr2Answer(t, e, d, kr2Kind(t, d, "yes")), "search")
		if d.Min != 0 || d.Max != 1 || len(d.Options) != 2 || d.Options[0].Obj != ids[0] || d.Options[1].Obj != ids[1] {
			t.Fatalf("pick = %+v, want a Min 0 search over both creatures", d)
		}
		if d = kr2Answer(t, e, d, kr2ObjIdx(t, d, ids[1])); d != nil {
			t.Fatalf("unexpected ask: %+v", d)
		}
		kr2RequireZones(t, e, ids[1:], state.ZHand, "answered creature")
		kr2RequireZones(t, e, ids[:1], state.ZLibrary, "unchosen creature")
	})
	t.Run("accept may find nothing", func(t *testing.T) {
		t.Parallel()
		e, _, ids, d := kr2SearchFetch(t, kr2SearchOptional, false)
		d = kr2Want(t, kr2Answer(t, e, d, kr2Kind(t, d, "yes")), "search")
		if d = kr2Answer(t, e, d); d != nil {
			t.Fatalf("unexpected ask: %+v", d)
		}
		kr2RequireZones(t, e, ids, state.ZLibrary, "accept-then-nothing")
	})
	for _, accept := range []bool{false, true} {
		name := "empty pool confirms, decline skips the tail"
		if accept {
			name = "empty pool confirms, accept owes the may-shuffle tail"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			e, _, _, d := kr2SearchFetch(t, kr2SearchOptional+" | ShuffleNonMandatory$ True", true)
			kr2RequireConfirm(t, d, "search_confirm")
			from := len(e.L.Events)
			if !accept {
				if d = kr2Answer(t, e, d, kr2Kind(t, d, "no")); d != nil {
					t.Fatalf("a declined empty-pool search posed %+v", d)
				}
				for _, ev := range kr2Events(e, from, events.Shuffle) {
					if ev.Player == 0 {
						t.Fatalf("a declined search shuffled: %+v", ev)
					}
				}
				return
			}
			d = kr2Answer(t, e, d, kr2Kind(t, d, "yes"))
			if d != nil && d.ResumeKind == "search" && len(d.Options) == 0 {
				d = kr2Answer(t, e, d)
			}
			kr2Want(t, d, "search_mayshuffle")
			for _, ev := range kr2Events(e, from, events.Shuffle) {
				if ev.Player == 0 {
					t.Fatalf("the tail shuffled before its answer: %+v", ev)
				}
			}
		})
	}
	t.Run("markerless text-may stays confirmation-free", func(t *testing.T) {
		t.Parallel()
		_, _, _, d := kr2SearchFetch(t, "A:SP$ ChangeZone | Origin$ Library | Destination$ Hand | ChangeType$ Creature | ChangeNum$ 1 | SpellDescription$ Search your library for a creature card, then shuffle.", false)
		if kr2Want(t, d, "search"); d.Min != 0 || d.Max != 1 {
			t.Fatalf("markerless first ask = %+v, want the Min 0 search", d)
		}
	})
	t.Run("ChoiceOptional$ does not confirm", func(t *testing.T) {
		t.Parallel()
		_, _, _, d := kr2SearchFetch(t, "A:SP$ ChangeZone | Origin$ Library | Destination$ Hand | ChangeType$ Creature | ChangeNum$ 1 | ChoiceOptional$ True", false)
		kr2Want(t, d, "search")
	})
	t.Run("object-valued Defined$ keeps its own election", func(t *testing.T) {
		t.Parallel()
		e, _, ids, d := kr2SearchFetch(t, "A:SP$ ChangeZone | Origin$ Library | Destination$ Hand | Defined$ TopOfLibrary | Optional$ True", false)
		kr2Want(t, d, "defined_library_optional")
		if d = kr2Answer(t, e, d, kr2Kind(t, d, "yes")); d != nil {
			t.Fatalf("the object-valued fetch posed a second ask %+v (never a search_confirm)", d)
		}
		kr2RequireZones(t, e, ids[:1], state.ZHand, "the defined top card")
	})
}

func TestPathToExileSearchConfirmsBeforeTheBasicLandSearch(t *testing.T) {
	t.Parallel()
	e := kr2Engine(t, 2)
	victim := kr2Put(t, e, 1, kr2Src(t, kr2BearSrc), state.ZBattlefield, false)
	path := kr2Put(t, e, 0, kr2Corpus(t, "Path to Exile"), state.ZHand, false)
	d := kr2Cast(t, e, 0, path)
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("cast = %+v, want the creature target ask", d)
	}
	d = kr2Want(t, kr2Answer(t, e, d, kr2ObjIdx(t, d, victim)), "search_confirm")
	if z := e.G.Obj(victim).Zone; z != state.ZExile {
		t.Fatalf("victim is on %s, want exile", z)
	}
	// "Its controller may search their library": the exiled creature's
	// controller (seat 1) decides and picks, never the caster (Forge's
	// decider is the DefinedPlayer$ fetcher absent a Chooser$).
	if d.Player != 1 {
		t.Fatalf("search_confirm decider = seat %d, want the exiled creature's controller 1", d.Player)
	}
	lands := len(e.G.Zone(state.ZBattlefield, 1))
	d = kr2Want(t, kr2Answer(t, e, d, kr2Kind(t, d, "yes")), "search")
	if d.Player != 1 {
		t.Fatalf("search pick decider = seat %d, want the searching player 1", d.Player)
	}
	for i := 0; d != nil && i < 4; i++ {
		if d.ResumeKind == "search" {
			d = kr2Answer(t, e, d, d.Options[0].Index)
			continue
		}
		d = kr2Answer(t, e, d, d.Options[0].Index)
	}
	if got := len(e.G.Zone(state.ZBattlefield, 1)); got != lands+1 {
		t.Fatalf("seat 1 battlefield = %d, want %d (the basic land arrived)", got, lands+1)
	}
}

// kr2MultiSearch builds a 3-seat game whose opponents each have a creature
// on top of their library, and casts an Optional$ search over both
// opponents.
func kr2MultiSearch(t *testing.T) (*Engine, [3]state.ObjID, *decision.Decision) {
	t.Helper()
	e := kr2Engine(t, 3)
	var bears [3]state.ObjID
	for p := state.PlayerID(1); p <= 2; p++ {
		bears[p] = kr2Put(t, e, p, kr2Src(t, kr2BearSrc), state.ZLibrary, true)
	}
	spell := kr2Put(t, e, 0, kr2Sorcery(t, "Searcher",
		"A:SP$ ChangeZone | Origin$ Library | Destination$ Hand | ChangeType$ Creature | ChangeNum$ 1 | DefinedPlayer$ Opponent | Optional$ True"), state.ZHand, false)
	return e, bears, kr2Cast(t, e, 0, spell)
}

func TestOptionalLibrarySearchMultiPlayer(t *testing.T) {
	t.Parallel()
	t.Run("decline skips only the answering player", func(t *testing.T) {
		t.Parallel()
		e, bears, d := kr2MultiSearch(t)
		if kr2Want(t, d, "search_confirm"); d.ResumeTarget != 0 {
			t.Fatalf("first confirmation = %+v, want cursor 0", d)
		}
		d = kr2Want(t, kr2Answer(t, e, d, kr2Kind(t, d, "no")), "search_confirm")
		if d.ResumeTarget != 1 {
			t.Fatalf("second confirmation = %+v, want cursor 1", d)
		}
		d = kr2Want(t, kr2Answer(t, e, d, kr2Kind(t, d, "yes")), "search")
		if len(d.Options) != 1 || d.Options[0].Obj != bears[2] {
			t.Fatalf("the second opponent's search offered %+v, want its own creature %d", d.Options, bears[2])
		}
		if d = kr2Answer(t, e, d, 0); d != nil {
			t.Fatalf("unexpected ask: %+v", d)
		}
		kr2RequireZones(t, e, bears[1:2], state.ZLibrary, "the declining opponent's creature")
		kr2RequireZones(t, e, bears[2:], state.ZHand, "the accepting opponent's creature")
	})
	t.Run("accepted first player is not re-asked after its pick", func(t *testing.T) {
		t.Parallel()
		e, bears, d := kr2MultiSearch(t)
		kr2Want(t, d, "search_confirm")
		d = kr2Want(t, kr2Answer(t, e, d, kr2Kind(t, d, "yes")), "search")
		if d.ResumeTarget != 0 || len(d.Options) != 1 || d.Options[0].Obj != bears[1] {
			t.Fatalf("first opponent's pick = %+v, want its own library card", d)
		}
		d = kr2Want(t, kr2Answer(t, e, d, 0), "search_confirm")
		if d.ResumeTarget != 1 {
			t.Fatalf("third ask = %+v, want the second opponent's confirmation (cursor 1), not a re-ask", d)
		}
		kr2RequireZones(t, e, bears[1:2], state.ZHand, "the answered card")
	})
}
