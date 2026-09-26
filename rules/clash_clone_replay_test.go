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
	"reflect"
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

// TestClashPlacementCloneIndependence drives a real clash to the point where
// seat 0's placement has been applied and seat 1's election is pending, clones
// there, and proves the clone owns an independent snapshot of the sequential
// election: answering seat 1 on the CLONE first must leave the original's
// pending choice, cursor, revealed set, log head and library untouched, and the
// original's own (different) answer must then produce its own order and head.
func TestClashPlacementCloneIndependence(t *testing.T) {
	e := combatEngine(t)
	marvo := onBoardCard(t, e, 0, unblockedCorpusCard(t, "m/marvo_deep_operative.txt"))
	e.G.Obj(marvo).SummonSick = false

	// Rebuild seat 0's and seat 1's libraries as exactly two distinguishable
	// cards each: the revealed top plus one filler underneath.
	clearLibrary := func(p state.PlayerID) {
		for _, id := range e.G.Zone(state.ZLibrary, p) {
			e.G.Obj(id).Zone = state.ZExile
		}
		e.G.SetZone(state.ZLibrary, p, nil)
	}
	clearLibrary(0)
	clearLibrary(1)
	high := e.G.AddObject(card(t, "Name:Controller clash\nManaCost:5\nTypes:Creature Beast\nPT:5/5\nOracle:x\n"), 0)
	highf := e.G.AddObject(card(t, "Name:Controller filler\nManaCost:1\nTypes:Creature Beast\nPT:1/1\nOracle:x\n"), 0)
	low := e.G.AddObject(card(t, "Name:Opponent clash\nManaCost:0\nTypes:Creature Beast\nPT:1/1\nOracle:x\n"), 1)
	lowf := e.G.AddObject(card(t, "Name:Opponent filler\nManaCost:2\nTypes:Creature Beast\nPT:2/2\nOracle:x\n"), 1)
	clashLibraryTwo(t, e, 0, high.ID, highf.ID)
	clashLibraryTwo(t, e, 1, low.ID, lowf.ID)

	// Preconditions: the comparison really favours seat 0, and both libraries
	// are exactly the two cards the assertions below read.
	if e.G.Obj(high.ID).Face().ManaValue() <= e.G.Obj(low.ID).Face().ManaValue() {
		t.Fatalf("comparison precondition: high MV=%d, low MV=%d", e.G.Obj(high.ID).Face().ManaValue(), e.G.Obj(low.ID).Face().ManaValue())
	}
	if got := e.G.Zone(state.ZLibrary, 0); len(got) != 2 || got[0] != high.ID || got[1] != highf.ID {
		t.Fatalf("controller library precondition: %v", got)
	}
	if got := e.G.Zone(state.ZLibrary, 1); len(got) != 2 || got[0] != low.ID || got[1] != lowf.ID {
		t.Fatalf("opponent library precondition: %v", got)
	}

	e.emit(events.Event{Kind: events.DeclareAttackers, Player: 1, IDs: []state.ObjID{marvo}})
	e.putTriggersOnStack()
	e.resolveTop()

	// Seat 0's election: bottom, so its revealed card moves under the filler.
	answerClashPlacement(t, e, 0, "bottom")

	// Seat 1's election is now outstanding. This is the exact boundary to
	// clone at: the cursor has advanced past seat 0, both revealed ids are
	// still carried, and the engine's resume point holds the snapshot.
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "clash_placement" || d.Player != 1 {
		t.Fatalf("expected seat 1's placement ask, got %+v", d)
	}
	if d.ResumeClash == nil || d.ResumeClash.Cursor != 1 || len(d.ResumeClash.Players) != 2 || len(d.ResumeClash.Revealed) != 2 {
		t.Fatalf("seat 1 ask resume precondition: %+v", d.ResumeClash)
	}
	if d.ResumeClash.Revealed[0] == d.ResumeClash.Revealed[1] || d.ResumeClash.Revealed[0] != high.ID || d.ResumeClash.Revealed[1] != low.ID {
		t.Fatalf("seat 1 ask revealed set = %v, want two distinct [%d %d]", d.ResumeClash.Revealed, high.ID, low.ID)
	}
	if e.resume == nil || e.resume.clash == nil {
		t.Fatal("precondition: the pending ask has no resumePoint.clash snapshot to alias")
	}
	if e.resume.clash.Cursor != 1 {
		t.Fatalf("engine resume cursor = %d, want 1", e.resume.clash.Cursor)
	}
	origHead := e.L.Head()
	origLib0 := append([]state.ObjID(nil), e.G.Zone(state.ZLibrary, 0)...)
	origLib1 := append([]state.ObjID(nil), e.G.Zone(state.ZLibrary, 1)...)

	// Clone at the boundary.
	c := e.Clone()
	cd := c.Pending()
	if cd == nil || cd.ResumeKind != "clash_placement" || cd.Player != 1 {
		t.Fatalf("clone lost the pending placement ask: %+v", cd)
	}

	// The clone's values match the original's ...
	if !reflect.DeepEqual(cd.ResumeClash, d.ResumeClash) {
		t.Fatalf("clone pending resume = %+v, want %+v", cd.ResumeClash, d.ResumeClash)
	}
	if !reflect.DeepEqual(c.resume.clash, e.resume.clash) {
		t.Fatalf("clone resumePoint.clash = %+v, want %+v", c.resume.clash, e.resume.clash)
	}
	// ... but it must not share the snapshot pointer nor the Players/Revealed
	// backing arrays with the original (a shallow copy would alias them).
	if c.resume.clash == e.resume.clash {
		t.Fatal("clone aliases the original's resumePoint.clash snapshot")
	}
	if cd.ResumeClash == d.ResumeClash {
		t.Fatal("clone's pending decision aliases the original's ResumeClash snapshot")
	}
	if &c.resume.clash.Players[0] == &e.resume.clash.Players[0] {
		t.Fatal("clone shares the resumePoint.clash Players backing array")
	}
	if &c.resume.clash.Revealed[0] == &e.resume.clash.Revealed[0] {
		t.Fatal("clone shares the resumePoint.clash Revealed backing array")
	}
	if &cd.ResumeClash.Players[0] == &d.ResumeClash.Players[0] {
		t.Fatal("clone shares the pending decision's Players backing array")
	}
	if &cd.ResumeClash.Revealed[0] == &d.ResumeClash.Revealed[0] {
		t.Fatal("clone shares the pending decision's Revealed backing array")
	}

	// Answer seat 1 on the CLONE first: bottom. The clone's own library order
	// changes; the clone must NOT touch the original.
	submitChoices(t, c, clashPlacementSelect(t, cd, "bottom"))

	if got := c.G.Zone(state.ZLibrary, 1); len(got) != 2 || got[0] != lowf.ID || got[1] != low.ID {
		t.Fatalf("clone seat 1 bottom order = %v, want [%d %d]", got, lowf.ID, low.ID)
	}

	// The original is untouched: same pending choice, cursor, revealed set,
	// library and log head.
	od := e.Pending()
	if od == nil || od.ResumeKind != "clash_placement" || od.Player != 1 {
		t.Fatalf("original's pending ask was consumed/changed by the clone: %+v", od)
	}
	if od.ResumeClash == nil || od.ResumeClash.Cursor != 1 || !reflect.DeepEqual(od.ResumeClash.Revealed, d.ResumeClash.Revealed) {
		t.Fatalf("original's resume snapshot changed by the clone: %+v", od.ResumeClash)
	}
	if got := e.L.Head(); got != origHead {
		t.Fatalf("original log head moved by answering the clone: %s, want %s", got, origHead)
	}
	if got := e.G.Zone(state.ZLibrary, 0); !reflect.DeepEqual(got, origLib0) {
		t.Fatalf("original seat 0 library changed by the clone: %v, want %v", got, origLib0)
	}
	if got := e.G.Zone(state.ZLibrary, 1); !reflect.DeepEqual(got, origLib1) {
		t.Fatalf("original seat 1 library changed by answering the clone: %v, want %v", got, origLib1)
	}

	// Now answer the original with the OTHER placement: top leaves seat 1's
	// order unchanged. The two engines therefore end with different libraries
	// and different chain heads -- impossible if they shared one snapshot.
	submitChoices(t, e, clashPlacementSelect(t, od, "top"))

	if got := e.G.Zone(state.ZLibrary, 1); len(got) != 2 || got[0] != low.ID || got[1] != lowf.ID {
		t.Fatalf("original seat 1 top order = %v, want [%d %d]", got, low.ID, lowf.ID)
	}
	if e.L.Head() == c.L.Head() {
		t.Fatal("clone and original end with the same chain head despite opposite seat 1 answers; the snapshot was shared")
	}
	if hasNote(e, "unimplemented API Clash") {
		t.Fatal("Clash handler was not registered")
	}
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
