package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestSunbirdsInvocationRestRandomOrderShufflesLibraryBottom is the
// ticket-20260918T221252Z-064ef40a pin for the brief's named real-corpus
// carrier: Sunbird's Invocation's `DBRestRandomOrder` ChangeZoneAll
// (`ChangeType$ Card.IsRemembered | Origin$ Library | Destination$ Library |
// LibraryPosition$ -1 | RandomOrder$ True`) returns the remembered revealed
// cards to the bottom of their owner's library in a shuffled order, and a
// log-only replay re-derives the identical order (the shuffle draws the
// seeded engine rng, never math/rand).
func TestSunbirdsInvocationRestRandomOrderShufflesLibraryBottom(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	sunbird := searchCorpusCard(t, reg, "Sunbird's Invocation")
	e, cfg := mordorEngine(t, reg, 4242, "Grizzly Bears", "Grizzly Bears", "Grizzly Bears", "Grizzly Bears")
	src := moveToBattlefieldByName(t, e, 0, "Grizzly Bears")
	// The remainder's `Card.IsRemembered` set is what PeekAndReveal's
	// RememberRevealed$ would have populated; feed three library cards into
	// it directly, still IN the library (Sunbird's Origin$ is Library).
	var revealed []state.Target
	for i := 0; i < 3; i++ {
		id := searchMoveByName(t, e, "Grizzly Bears", state.ZLibrary)
		revealed = append(revealed, state.Target{Obj: id})
	}
	if len(e.G.Zone(state.ZLibrary, 0)) < len(revealed)+1 {
		t.Fatalf("library too small: %d", len(e.G.Zone(state.ZLibrary, 0)))
	}
	e.priorityRound()
	sa := cards.ResolveSVar(sunbird.Faces[0].SVars, "DBRestRandomOrder")
	if sa == nil {
		t.Fatal("Sunbird's Invocation's DBRestRandomOrder SVar did not resolve")
	}
	if sa.API != "ChangeZoneAll" {
		t.Fatalf("resolved SVar API = %q, want ChangeZoneAll", sa.API)
	}
	ctx := &effects.Ctx{Source: src, Controller: 0, Remembered: revealed, ResolvingObj: src}
	effects.Resolve(e, ctx, sa)
	lib := e.G.Zone(state.ZLibrary, 0)
	if len(lib) < len(revealed) {
		t.Fatalf("library %d shorter than the revealed set %d", len(lib), len(revealed))
	}
	bottom := lib[len(lib)-len(revealed):]
	seen := map[state.ObjID]bool{}
	for _, id := range bottom {
		seen[id] = true
	}
	for _, tgt := range revealed {
		if !seen[tgt.Obj] {
			t.Fatalf("revealed card %d is not at the library bottom: bottom=%v", tgt.Obj, bottom)
		}
	}
	if len(seen) != len(revealed) {
		t.Fatalf("the bottom placement duplicated ids: %v", bottom)
	}
	// The RANDOM part is only pinned by demanding the shuffle diverged from
	// the collection (zone scan) order; a permutation test passes even if
	// RandomOrder$ is dropped.
	same := true
	for i, tgt := range revealed {
		if bottom[i] != tgt.Obj {
			same = false
			break
		}
	}
	if same {
		t.Fatalf("bottom order equals the reveal order — RandomOrder$ did not shuffle: bottom=%v", bottom)
	}
	replayCheck(t, e, cfg)
}

// TestTriumphOfSaintKatherineRandomOrderOnTop pins the defect the
// ticket-20260918T221252Z-064ef40a review found in mordorparams1's
// RandomOrder$ branch: the branch returned BEFORE the shared post-placement
// tail, so `LibraryPosition$ 0` was ignored and a RandomOrder$ sweep always
// landed at the BOTTOM. Triumph of Saint Katherine's real compiled
// DBChangeLibrary is `... | Destination$ Library | LibraryPosition$ 0 |
// RandomOrder$ True` and its oracle is "shuffle that pile and put it back on
// TOP of your library", so the shuffled pile must sit above the pre-existing
// library.
func TestTriumphOfSaintKatherineRandomOrderOnTop(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	triumph := searchCorpusCard(t, reg, "Triumph of Saint Katherine")
	e, cfg := mordorEngine(t, reg, 7701, "Grizzly Bears", "Grizzly Bears", "Grizzly Bears", "Grizzly Bears", "Grizzly Bears", "Grizzly Bears", "Grizzly Bears", "Grizzly Bears", "Grizzly Bears", "Grizzly Bears", "Grizzly Bears", "Grizzly Bears")
	src := moveToBattlefieldByName(t, e, 0, "Grizzly Bears")
	// The compiled DBChangeLibrary carries `ConditionCheckSVar$ RememberedSize |
	// ConditionSVarCompare$ GE7` (the real trigger remembers the creature plus
	// the six dug cards), so the pool must hold at least seven for the gap
	// gate to open — the exact reason this pin feeds seven, not three.
	var exiled []state.Target
	for i := 0; i < 7; i++ {
		id := searchMoveByName(t, e, "Grizzly Bears", state.ZHand)
		e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZExile, Text: "exiled"})
		// The compiled gate reads the SOURCE's event-backed remembered list
		// (Count$RememberedSize), exactly what the chain's own
		// RememberChanged$ rider writes; record each card there too.
		e.emit(events.Event{Kind: events.Choose, Obj: src, Counter: "remembered", IDs: []state.ObjID{id}})
		exiled = append(exiled, state.Target{Obj: id})
	}
	// A pre-existing library card that must end up BELOW the shuffled pile.
	below := searchMoveByName(t, e, "Grizzly Bears", state.ZLibrary)
	if len(e.G.Zone(state.ZLibrary, 0)) < 2 {
		t.Fatalf("library too small for the on-top assertion: %d", len(e.G.Zone(state.ZLibrary, 0)))
	}
	e.priorityRound()
	sa := cards.ResolveSVar(triumph.Faces[0].SVars, "DBChangeLibrary")
	if sa == nil {
		t.Fatal("Triumph of Saint Katherine's DBChangeLibrary SVar did not resolve")
	}
	if !equalFold(sa.Params["LibraryPosition"], "0") || !equalFold(sa.Params["RandomOrder"], "True") {
		t.Fatalf("compiled SA lost its params: LibraryPosition=%q RandomOrder=%q",
			sa.Params["LibraryPosition"], sa.Params["RandomOrder"])
	}
	ctx := &effects.Ctx{Source: src, Controller: 0, Remembered: exiled, ResolvingObj: src}
	effects.Resolve(e, ctx, sa)
	lib := e.G.Zone(state.ZLibrary, 0)
	// PRECONDITION: all three returned cards are present, and the pre-existing
	// card is present, so the position assertion below is not vacuous.
	inLib := func(id state.ObjID) bool {
		for _, x := range lib {
			if x == id {
				return true
			}
		}
		return false
	}
	for _, tgt := range exiled {
		if !inLib(tgt.Obj) {
			t.Fatalf("returned card %d not in the library: %v", tgt.Obj, lib)
		}
	}
	if !inLib(below) {
		t.Fatalf("pre-existing library card %d vanished: %v", below, lib)
	}
	top := lib[:len(exiled)]
	seen := map[state.ObjID]bool{}
	for _, id := range top {
		seen[id] = true
	}
	for _, tgt := range exiled {
		if !seen[tgt.Obj] {
			t.Fatalf("returned card %d is not in the top %d (LibraryPosition$ 0 ignored): lib=%v",
				tgt.Obj, len(exiled), lib)
		}
	}
	if len(seen) != len(exiled) {
		t.Fatalf("top placement duplicated ids: %v", top)
	}
	// The pre-existing card must be strictly BELOW the pile.
	if seen[below] {
		t.Fatalf("pre-existing card %d landed in the shuffled top pile: lib=%v", below, lib)
	}
	replayCheck(t, e, cfg)
}

// equalFold is a tiny local helper so the test does not import strings just
// for two comparisons.
func equalFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if 'A' <= ca && ca <= 'Z' {
			ca += 'a' - 'A'
		}
		if 'A' <= cb && cb <= 'Z' {
			cb += 'a' - 'A'
		}
		if ca != cb {
			return false
		}
	}
	return true
}
