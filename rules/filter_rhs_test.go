package rules

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// The resolution-time numeric-RHS carrier tests. A filter spec whose numeric
// bound is not a literal -- Creature.powerGTX, Artifact.cmcLEX -- used to
// never match at resolution: the only resolver Ctx.SpecContext installed was
// the roll-publication one, so Nightmare Unmaking exiled nothing in either
// mode and Whir of Invention found no artifact. Both pins below drive the
// REAL corpus cards through the engine (the Valiant-Endeavor
// drain-and-submit harness), never a synthetic SVar probe.

// exileNames returns the sorted display names of every object sitting in any
// seat's exile zone.
func exileNames(t *testing.T, e *Engine) []string {
	t.Helper()
	var names []string
	for _, p := range e.G.AliveFrom(0) {
		for _, id := range e.G.Zone(state.ZExile, p) {
			o := e.G.Obj(id)
			if o == nil || o.Face() == nil {
				continue
			}
			names = append(names, o.Face().Name)
		}
	}
	sort.Strings(names)
	return names
}

// bfNames returns the sorted display names of every creature on every
// battlefield (each name once per object).
func bfNames(t *testing.T, e *Engine) []string {
	t.Helper()
	var names []string
	for _, p := range e.G.AliveFrom(0) {
		for _, id := range e.G.Zone(state.ZBattlefield, p) {
			o := e.G.Obj(id)
			if o == nil || o.Face() == nil || !o.Face().IsCreature() {
				continue
			}
			names = append(names, fmt.Sprintf("%s@%d", o.Face().Name, p))
		}
	}
	sort.Strings(names)
	return names
}

// shrinkHandTo moves hand cards back onto their owner's library (the same
// moveByName path every other fixture uses) until seat p's hand holds exactly
// n cards, never moving the named keep card. The count of cards in hand is
// what Nightmare Unmaking's SVar:X:Count$ValidHand Card.YouOwn evaluates, so
// the test fixes it exactly.
func shrinkHandTo(t *testing.T, e *Engine, p state.PlayerID, n int, keep string) {
	t.Helper()
	for range 20 {
		hand := e.G.Zone(state.ZHand, p)
		if len(hand) <= n {
			break
		}
		var pick *state.Object
		for _, id := range hand {
			o := e.G.Obj(id)
			if o == nil || o.Face() == nil || o.Face().Name == keep {
				continue
			}
			if pick == nil || o.Face().Name == "Mountain" {
				pick = o
				if o.Face().Name == "Mountain" {
					break
				}
			}
		}
		if pick == nil {
			t.Fatalf("hand of %d cards holds nothing but %q to move back", len(hand), keep)
		}
		moveByName(t, e, p, pick.Face().Name, state.ZLibrary)
	}
	hand := e.G.Zone(state.ZHand, p)
	if len(hand) != n {
		t.Fatalf("hand size %d, want %d", len(hand), n)
	}
}

// runNightmareUnmaking casts the real corpus Nightmare Unmaking, answers its
// Charm mode announcement (0 = "power greater than", 1 = "power less than"),
// drains the stack, and returns the engine for the board assertions. The
// hand is FOUR cards before the cast so it is exactly THREE at resolution --
// the spell itself has left the hand by then, and "the number of cards in
// your hand" is what SVar:X:Count$ValidHand Card.YouOwn evaluates at that
// moment. Creatures sit on both battlefields around the boundary: Llanowar
// Elves (1), Grizzly Bears (2), Hill Giant (3) for seat 0, Llanowar Elves (1)
// and Craw Wurm (6) for seat 1.
func runNightmareUnmaking(t *testing.T, mode int) (*Engine, Config) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	e, cfg := corpusEngineCfg(t, reg,
		[]*cards.Card{lookup(t, reg, "Nightmare Unmaking"), lookup(t, reg, "Llanowar Elves"),
			lookup(t, reg, "Grizzly Bears"), lookup(t, reg, "Hill Giant")},
		[]*cards.Card{lookup(t, reg, "Llanowar Elves"), lookup(t, reg, "Craw Wurm")})
	moveByName(t, e, 0, "Nightmare Unmaking", state.ZHand)
	moveByName(t, e, 0, "Llanowar Elves", state.ZBattlefield)
	moveByName(t, e, 0, "Grizzly Bears", state.ZBattlefield)
	moveByName(t, e, 0, "Hill Giant", state.ZBattlefield)
	moveByName(t, e, 1, "Llanowar Elves", state.ZBattlefield)
	moveByName(t, e, 1, "Craw Wurm", state.ZBattlefield)
	shrinkHandTo(t, e, 0, 4, "Nightmare Unmaking")

	addMana(t, e, 0, "BBBBBB")
	e.priorityRound()
	castNamed(t, e, "Nightmare Unmaking")

	// CR 601.2b: the Charm's mode is announced while casting, before the
	// spell sits on the stack -- the same announcement shape
	// nested_charm_modes_test.go pins.
	d := e.Pending()
	if d == nil || d.Kind != decision.KModes || len(d.Options) != 2 {
		t.Fatalf("mode ask after the cast: %+v", d)
	}
	if !strings.Contains(d.Options[0].Label, "greater than") ||
		!strings.Contains(d.Options[1].Label, "less than") {
		t.Fatalf("mode option order: %q / %q", d.Options[0].Label, d.Options[1].Label)
	}
	submitChoices(t, e, mode)
	passUntilStackEmpty(t, e, 40)
	if got := len(e.G.Zone(state.ZHand, 0)); got != 3 {
		t.Fatalf("hand at resolution = %d, want 3", got)
	}
	return e, cfg
}

// TestNightmareUnmakingExilesAboveHandSize is mode 1: with exactly 3 cards in
// hand, "exile each creature with power greater than the number of cards in
// your hand" exiles exactly the Craw Wurm (power 6), keeps the power-3 Hill
// Giant (the boundary is strict) and every creature at or below 3, on BOTH
// battlefields -- and the answered game replays byte-identically.
func TestNightmareUnmakingExilesAboveHandSize(t *testing.T) {
	t.Parallel()
	e, cfg := runNightmareUnmaking(t, 0)
	got := exileNames(t, e)
	want := []string{"Craw Wurm"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("exile zone = %v, want %v", got, want)
	}
	gotBF := bfNames(t, e)
	wantBF := []string{"Grizzly Bears@0", "Hill Giant@0", "Llanowar Elves@0", "Llanowar Elves@1"}
	if fmt.Sprint(gotBF) != fmt.Sprint(wantBF) {
		t.Fatalf("battlefields = %v, want %v", gotBF, wantBF)
	}
	replayCheck(t, e, cfg)
}

// TestNightmareUnmakingExilesBelowHandSize is mode 2: with exactly 3 cards in
// hand at resolution, "exile each creature with power less than" exiles
// exactly the four sub-3 creatures (both seats' Llanowar Elves and seat 0's
// Grizzly Bears) and keeps the power-3 Hill Giant and the power-6 Craw Wurm;
// the answered game replays byte-identically.
func TestNightmareUnmakingExilesBelowHandSize(t *testing.T) {
	t.Parallel()
	e, cfg := runNightmareUnmaking(t, 1)
	got := exileNames(t, e)
	want := []string{"Grizzly Bears", "Llanowar Elves", "Llanowar Elves"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("exile zone = %v, want %v", got, want)
	}
	gotBF := bfNames(t, e)
	wantBF := []string{"Craw Wurm@1", "Hill Giant@0"}
	if fmt.Sprint(gotBF) != fmt.Sprint(wantBF) {
		t.Fatalf("battlefields = %v, want %v", gotBF, wantBF)
	}
	replayCheck(t, e, cfg)
}

// TestWhirOfInventionSearchesUpToThePaidX casts the real corpus Whir of
// Invention with a REAL announced X of 3 and asserts the hidden-library
// search's Artifact.cmcLEX cap resolves against the PAID X: the search
// offers exactly the two mana-value<=3 artifacts and not the mana-value-6
// Wurmcoil Engine, and the answered pick (Sol Ring, the non-first eligible)
// reaches the battlefield while both unchosen artifacts stay in the library.
func TestWhirOfInventionSearchesUpToThePaidX(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := corpusEngineCfg(t, reg,
		[]*cards.Card{lookup(t, reg, "Whir of Invention"), lookup(t, reg, "Sol Ring"),
			lookup(t, reg, "Mind Stone"), lookup(t, reg, "Wurmcoil Engine")},
		[]*cards.Card{})
	moveByName(t, e, 0, "Whir of Invention", state.ZHand)
	moveByName(t, e, 0, "Wurmcoil Engine", state.ZLibrary) // the mv-6 control, capped out of the search
	addMana(t, e, 0, "UUUUUU")                             // X=3 plus the three {U} pips
	e.priorityRound()
	castNamed(t, e, "Whir of Invention")

	// CR 601.2b: the X announcement ask, six mana pricing X 0..3.
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || len(d.Options) != 4 ||
		d.Options[0].Kind != "x" || d.Options[3].Label != "X = 3" {
		t.Fatalf("X ask after the cast: %+v", d)
	}
	submitChoices(t, e, 3)

	// The search ask comes at RESOLUTION: pass priority until it suspends.
	d = passUntilNonPriority(t, e, 40)
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "search" {
		t.Fatalf("search ask after X=3: %+v", d)
	}
	var labels []string
	solRing := -1
	for _, o := range d.Options {
		labels = append(labels, o.Label)
		if o.Label == "Sol Ring" {
			solRing = o.Index
		}
	}
	if solRing < 0 {
		t.Fatalf("search options %v do not offer the mv-1 Sol Ring", labels)
	}
	for _, o := range d.Options {
		if o.Label == "Wurmcoil Engine" {
			t.Fatalf("search offered the mv-6 Wurmcoil Engine at X=3: %v", labels)
		}
	}
	if len(labels) != 2 { // Sol Ring (1) and Mind Stone (2); the Engine is capped out
		t.Fatalf("search options = %v, want exactly [Mind Stone Sol Ring]", labels)
	}
	submitChoices(t, e, solRing)
	passUntilStackEmpty(t, e, 40)

	// Sol Ring on the battlefield; both unchosen artifacts still in the library.
	if o := findObjByName(t, e, "Sol Ring"); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("Sol Ring not on the battlefield after the search (zone %v)", o)
	}
	var lib []string
	for _, id := range e.G.Zone(state.ZLibrary, 0) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil {
			lib = append(lib, o.Face().Name)
		}
	}
	for _, name := range []string{"Mind Stone", "Wurmcoil Engine"} {
		found := false
		for _, l := range lib {
			if l == name {
				found = true
			}
		}
		if !found {
			t.Fatalf("%s left the library after the X=3 search (library holds %d cards)", name, len(lib))
		}
	}
	for _, l := range lib {
		if l == "Sol Ring" {
			t.Fatal("Sol Ring is still in the library after being fetched")
		}
	}
	replayCheck(t, e, cfg)
}
