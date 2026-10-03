package rules

// CR 701.31 clash placement: two contracts the existing clash fixtures do not
// pin.
//
//  1. Clone mid-election (TestClashPlacementCloneIndependence). The sequential
//     owner elections resume from a saved reveal snapshot (decision.ClashResume)
//     carried on both the pending decision and the engine's resume point. When
//     seat 0's answer has been applied and seat 1's election is still pending,
//     Engine.Clone must hand the copy its OWN snapshot: answering the copy must
//     not consume or mutate the original's pending choice. rules/resolution.go's
//     cloneClashResume / rules/clone.go's cloneResume+cloneDecision are the code
//     under test; the card-copy api:Clone has its own tests and is unrelated.
//
//  2. Log-only replay (TestClashPlacementLogOnlyReplay). The whole clash -- both
//     reveals, both owner elections and the resulting library orders -- must be
//     reconstructible from the event log alone (no direct state.Game edits, no
//     putTopOfLibrary), so replayCheck's fold-from-genesis reproduces the board.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// clashPlacementSelect returns the option index whose Kind is want.
func clashPlacementSelect(t *testing.T, d *decision.Decision, want string) int {
	t.Helper()
	for _, o := range d.Options {
		if o.Kind == want {
			return o.Index
		}
	}
	t.Fatalf("placement options = %+v, missing %q", d.Options, want)
	return -1
}

// clashLibraryTwo seeds seat p's library with exactly [top, bottom] and
// returns both ids, mirroring the direct-edit shape the non-replay clash
// fixtures use. Only valid for a fixture that does not need replayCheck.
func clashLibraryTwo(t *testing.T, e *Engine, p state.PlayerID, top, bottom state.ObjID) {
	t.Helper()
	for _, id := range e.G.Zone(state.ZLibrary, p) {
		e.G.Obj(id).Zone = state.ZExile
	}
	e.G.SetZone(state.ZLibrary, p, []state.ObjID{top, bottom})
	e.G.Obj(top).Zone = state.ZLibrary
	e.G.Obj(bottom).Zone = state.ZLibrary
}

// clashReplayHigh/ClashReplayLow are the inline filler cards the log-only
// replay fixture parks on each library top; their mana values differ so the
// comparison is observable.
const (
	clashReplayHighSrc = "Name:Clash Replay High\nManaCost:5\nTypes:Creature Beast\nPT:5/5\nOracle:x\n"
	clashReplayLowSrc  = "Name:Clash Replay Low\nManaCost:0\nTypes:Creature Beast\nPT:1/1\nOracle:x\n"
)

// clashReplayEngine builds a two-seat game whose cfg can be handed to
// replayCheck. Marvo is moved to the battlefield through a logged MoveZone
// (searchMoveByName) at turn 1, and the two revealed cards are parked on each
// library top through a logged MoveZone plus a complete secret LibraryOrder
// (seatLibraryTop) at turn 3's declare-attackers step, after every draw that
// could take them.
func clashReplayEngine(t *testing.T, reg *cards.Registry) (*Engine, Config, state.ObjID) {
	t.Helper()
	marvo := lookup(t, reg, "Marvo, Deep Operative")
	high := card(t, clashReplayHighSrc)
	low := card(t, clashReplayLowSrc)
	mountain := lookup(t, reg, "Mountain")

	deck0 := []*cards.Card{marvo, high, high, high}
	for len(deck0) < 40 {
		deck0 = append(deck0, mountain)
	}
	deck1 := []*cards.Card{low, low, low, low}
	for len(deck1) < 40 {
		deck1 = append(deck1, mountain)
	}
	cfg := seatZeroStart(Config{Seed: 7703, Names: []string{"clasher", "defender"},
		Decks: [][]*cards.Card{deck0, deck1}, Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)

	marvoID := searchMoveByName(t, e, "Marvo, Deep Operative", state.ZBattlefield)
	// Drive to seat 0's third turn, past both seats' draw steps, so the parked
	// tops are not drawn away and Marvo's summoning sickness has cleared.
	driveToStepAll(t, e, 3, 0, state.StepDeclareAttackers)
	highID := seatLibraryTop(t, e, 0, "Clash Replay High")
	lowID := seatLibraryTop(t, e, 1, "Clash Replay Low")

	o := e.G.Obj(marvoID)
	if o == nil || o.Zone != state.ZBattlefield || o.Face() == nil || o.Face().Name != "Marvo, Deep Operative" {
		t.Fatalf("Marvo precondition: %+v", o)
	}
	if o.SummonSick {
		t.Fatal("Marvo precondition: still summoning sick at turn 3 (a direct-write assumption leaked in)")
	}
	if hi, lo := e.G.Obj(highID).Face().ManaValue(), e.G.Obj(lowID).Face().ManaValue(); hi <= lo {
		t.Fatalf("clash comparison precondition: high MV=%d, low MV=%d", hi, lo)
	}
	if got := e.G.Zone(state.ZLibrary, 0); len(got) < 2 || got[0] != highID {
		t.Fatalf("seat 0 library-top precondition: %v, want %d first", got, highID)
	}
	if got := e.G.Zone(state.ZLibrary, 1); len(got) < 2 || got[0] != lowID {
		t.Fatalf("seat 1 library-top precondition: %v, want %d first", got, lowID)
	}
	return e, cfg, marvoID
}

// TestClashPlacementLogOnlyReplay runs the real Marvo clash entirely through
// logged events -- seeded decks, logged MoveZone placements, the engine's own
// DeclareAttackers path and Engine.Submit for both owner elections -- then
// folds the log from genesis and requires the board to match. Both owners
// choose bottom on a multi-card library, so a real secret LibraryOrder is
// emitted for each.
func TestClashPlacementLogOnlyReplay(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg, marvoID := clashReplayEngine(t, reg)

	// The declared attack goes through the engine's own declaration path (the
	// only one that captures the defending-player role Marvo's
	// `Defined$ TriggeredDefendingPlayer` reads).
	submitAttackersOnly(t, e, marvoID)
	drainCombatPriority(t, e)
	d := passUntilNonPriority(t, e, 80)
	if d == nil || d.ResumeKind != "clash_placement" || d.Player != 0 {
		t.Fatalf("expected seat 0's clash placement, got %+v", d)
	}

	highID := e.G.Zone(state.ZLibrary, 0)[0]
	lowID := e.G.Zone(state.ZLibrary, 1)[0]

	// Both owners elect bottom.
	answerClashPlacement(t, e, 0, "bottom")
	answerClashPlacement(t, e, 1, "bottom")

	// Both reveals are recorded, both markers exist, and the emitted
	// LibraryOrders actually reordered (one per participant).
	revealCount := map[state.ObjID]int{}
	markerCount := map[state.PlayerID]int{}
	orderCount := map[state.PlayerID]int{}
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && len(ev.IDs) == 1 {
			revealCount[ev.IDs[0]]++
		}
		if ev.Kind == events.Clash {
			markerCount[ev.Player]++
		}
		if ev.Kind == events.LibraryOrder {
			orderCount[ev.Player]++
		}
	}
	if revealCount[highID] != 1 || revealCount[lowID] != 1 {
		t.Fatalf("clash reveal notes = high:%d low:%d, want exactly one each", revealCount[highID], revealCount[lowID])
	}
	if markerCount[0] != 1 || markerCount[1] != 1 {
		t.Fatalf("clash markers = %v, want exactly one per seat", markerCount)
	}
	if orderCount[0] < 1 || orderCount[1] < 1 {
		t.Fatalf("LibraryOrders = %v, want at least one per participant (a bottom on a multi-card library)", orderCount)
	}
	if !hasClashEvent(e, 0, true) || !hasClashEvent(e, 1, false) {
		t.Fatal("clash orientation: seat 0's higher card must win and seat 1's must lose")
	}

	// Expected final order: each revealed card is now at the bottom of its
	// library, under whatever the private LibraryOrder preserved above it.
	lib0 := e.G.Zone(state.ZLibrary, 0)
	if lib0[len(lib0)-1] != highID || lib0[0] == highID {
		t.Fatalf("seat 0 bottom election order = %v, want revealed card %d on the bottom", lib0, highID)
	}
	lib1 := e.G.Zone(state.ZLibrary, 1)
	if lib1[len(lib1)-1] != lowID || lib1[0] == lowID {
		t.Fatalf("seat 1 bottom election order = %v, want revealed card %d on the bottom", lib1, lowID)
	}
	if hasNote(e, "unimplemented API Clash") {
		t.Fatal("Clash handler was not registered")
	}

	// The whole clash reconstructs from the log alone.
	replayCheck(t, e, cfg)
}
