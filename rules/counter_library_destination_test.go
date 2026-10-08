package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// counterLibraryDestinationHands sets up a two-seat game: seat 0 holds a
// Grizzly Bears, seat 1 holds the named counterspell (plus enough mana for it
// on both seats' side). It casts the Bear, then returns after seat 1 has
// targeted it with the counterspell but BEFORE the counter resolves, so the
// caller can assert the precondition (the Bear is on the stack, and seat 0's
// library has a known top card that is not the Bear) and then drain.
func counterLibraryDestinationHands(t *testing.T, e *Engine, counterName string, seedMana0, seedMana1 string) (bear, counter state.ObjID, targetIdx int) {
	t.Helper()
	addMana(t, e, 0, seedMana0)
	addMana(t, e, 1, seedMana1)
	bear = miscHandObj(t, e, 0, "Grizzly Bears")
	counter = miscHandObj(t, e, 1, counterName)

	submitChoices(t, e, miscCastOption(t, e, bear))
	miscPass(t, e)
	submitChoices(t, e, passToCast(t, e, counter))
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected a target decision for %s, got %+v", counterName, d)
	}
	targetIdx = -1
	for _, o := range d.Options {
		if o.Obj == bear {
			targetIdx = o.Index
		}
	}
	if targetIdx < 0 {
		t.Fatalf("the Bear spell was not offered as %s's target: %+v", counterName, d.Options)
	}
	return bear, counter, targetIdx
}

// TestCounterDestinationTopOfLibraryPutsTheCounteredSpellOnItsOwnersLibrary
// (Memory Lapse, Lapse of Certainty -- param:api:Counter.Destination): a
// Counter with Destination$ TopOfLibrary sends the countered card to the top
// of its OWNER's library instead of the graveyard. Before the fix effCounter
// knew only Hand/Graveyard/Exile and the TopOfLibrary word fell through to
// the default -- a Note plus the graveyard.
func TestCounterDestinationTopOfLibraryPutsTheCounteredSpellOnItsOwnersLibrary(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"Memory Lapse", "Lapse of Certainty"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			reg := searchTestRegistry(t)
			e, cfg := miscHandsEngine(t, reg,
				[]string{"Grizzly Bears"}, []string{name}, nil, nil)

			// Seat 0 needs {1}{G} for the Bear; both counterspells cost two
			// coloured mana of their own colour.
			bear, counter, targetIdx := counterLibraryDestinationHands(t, e, name, "GG", "UUWW")

			// PRECONDITION: the Bear is a spell ON THE STACK when targeted,
			// and seat 0's library is non-empty with a known top card that is
			// NOT the Bear. A vacuous setup (Bear already gone, empty library)
			// must fail loudly rather than let the real assertion pass.
			if o := e.G.Obj(bear); o == nil || o.Zone != state.ZStack {
				t.Fatalf("precondition: Bear = %+v, want it on the stack as the target", o)
			}
			lib0 := e.G.Zone(state.ZLibrary, 0)
			if len(lib0) == 0 {
				t.Fatal("precondition: seat 0's library is empty; the top-placement assertion could not fail")
			}
			topBefore := lib0[0]
			if topBefore == bear {
				t.Fatalf("precondition: the Bear is already the top card of seat 0's library")
			}

			submitChoices(t, e, targetIdx)
			passUntilStackEmpty(t, e, 30)

			// The countered Bear is in its OWNER's (seat 0's) library, at the
			// TOP, and not in the graveyard.
			if o := e.G.Obj(bear); o == nil || o.Zone != state.ZLibrary || o.Owner != 0 {
				t.Fatalf("%s-countered Bear = %+v, want it in seat 0's library", name, o)
			}
			after := e.G.Zone(state.ZLibrary, 0)
			if len(after) == 0 || after[0] != bear {
				t.Fatalf("top card of seat 0's library after %s = %v, want the countered Bear %d on top (lib=%v)",
					name, after, bear, after)
			}
			if n := countNamed(t, e, state.ZGraveyard, 0, "Grizzly Bears"); n != 0 {
				t.Fatalf("seat 0's graveyard holds %d Bears after the %s, want 0", n, name)
			}
			// The counterspell itself resolved and went to seat 1's graveyard.
			if o := e.G.Obj(counter); o == nil || o.Zone != state.ZGraveyard {
				t.Fatalf("resolved %s = %+v, want graveyard", name, o)
			}
			replayCheck(t, e, cfg)
		})
	}
}

// TestCounterDestinationBottomOfLibraryPutsTheCounteredSpellUnderItsOwnersLibrary
// (Spell Crumple -- param:api:Counter.Destination): a Counter with
// Destination$ BottomOfLibrary buries the countered card at the bottom of its
// owner's library. Spell Crumple's SubAbility$ DBChange then puts Spell
// Crumple itself on the bottom of ITS OWNER's library via ChangeZone
// (Origin$ Stack, Defined$ Parent), so the Crumple lands beneath the Bear.
func TestCounterDestinationBottomOfLibraryPutsTheCounteredSpellUnderItsOwnersLibrary(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := miscHandsEngine(t, reg,
		[]string{"Grizzly Bears"}, []string{"Spell Crumple"}, nil, nil)

	bear, counter, targetIdx := counterLibraryDestinationHands(t, e, "Spell Crumple", "GG", "UUU")

	// PRECONDITION: the Bear is on the stack when targeted and seat 0's
	// library is non-empty with a top card that is not the Bear.
	if o := e.G.Obj(bear); o == nil || o.Zone != state.ZStack {
		t.Fatalf("precondition: Bear = %+v, want it on the stack as the target", o)
	}
	lib0 := e.G.Zone(state.ZLibrary, 0)
	if len(lib0) == 0 {
		t.Fatal("precondition: seat 0's library is empty; the bottom-placement assertion could not fail")
	}
	if lib0[0] == bear {
		t.Fatalf("precondition: the Bear is already the top card of seat 0's library")
	}
	lib1 := e.G.Zone(state.ZLibrary, 1)
	if len(lib1) == 0 {
		t.Fatal("precondition: seat 1's library is empty; Spell Crumple's own bottom placement could not fail")
	}

	submitChoices(t, e, targetIdx)
	passUntilStackEmpty(t, e, 30)

	// The countered Bear is at the BOTTOM of its owner's (seat 0's) library,
	// never in the graveyard.
	if o := e.G.Obj(bear); o == nil || o.Zone != state.ZLibrary || o.Owner != 0 {
		t.Fatalf("Spell Crumple-countered Bear = %+v, want it in seat 0's library", o)
	}
	after0 := e.G.Zone(state.ZLibrary, 0)
	if len(after0) == 0 || after0[len(after0)-1] != bear {
		t.Fatalf("bottom card of seat 0's library after Spell Crumple = %v, want the countered Bear %d (lib=%v)",
			after0, bear, after0)
	}
	if n := countNamed(t, e, state.ZGraveyard, 0, "Grizzly Bears"); n != 0 {
		t.Fatalf("seat 0's graveyard holds %d Bears after Spell Crumple, want 0", n)
	}

	// Spell Crumple's own DBChange (Origin$ Stack, Destination$ Library,
	// LibraryPosition$ -1, Defined$ Parent) puts it at the bottom of seat 1's
	// library, AFTER the Bear was already placed at the bottom of seat 0's.
	// The two libraries are separate, so both assertions hold independently;
	// the Bear must also NOT be the very last card of seat 1's library (which
	// is where Crumple went).
	if o := e.G.Obj(counter); o == nil || o.Zone != state.ZLibrary {
		t.Fatalf("Spell Crumple itself = %+v, want it in seat 1's library (DBChange)", o)
	}
	after1 := e.G.Zone(state.ZLibrary, 1)
	if len(after1) == 0 || after1[len(after1)-1] != counter {
		t.Fatalf("bottom card of seat 1's library = %v, want Spell Crumple %d (lib=%v)", after1, counter, after1)
	}
	replayCheck(t, e, cfg)
}
