package rules

// The repl:Draw / repl:DrawCards replacement class (task
// cli-20261004T233421Z-ff9b2344). The matching and application machinery has
// existed since the Draw-replacement lane (rules/draw_replacement_test.go),
// but neither primitive was ever registered with effects.RegisterNonAPI, so
// every carrier's coverage walk reported "unsupported [repl:Draw]" and the
// Standard compliance audit skipped the card whole -- Bard, King of Dale
// (HOB), Living Conundrum (MKM) and Quantum Riddler (EOE). These pins drive
// the REAL corpus scripts through the engine so a revert of the registration
// (or of a matched parameter read the card relies on) fails here rather than
// silently shrinking the coverage ratchet.

import (
	"testing"

	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestReplDrawPrimitivesAreRegistered pins the effects.RegisterNonAPI
// registration: without it the coverage walk reports every Draw-class carrier
// as unsupported and the compliance audit skips the card whole, so a revert
// of the registration -- not just of a match arm -- must fail the build.
func TestReplDrawPrimitivesAreRegistered(t *testing.T) {
	t.Parallel()
	sup := effects.Supported()
	for _, prim := range []string{"repl:Draw", "repl:DrawCards"} {
		if !sup[prim] {
			t.Fatalf("effects.Supported() lacks %q -- the replacement_cmdzone.go registration was reverted", prim)
		}
	}
	reg := testutil.CorpusRegistry(t)
	for _, n := range []string{"Bard, King of Dale", "Living Conundrum", "Quantum Riddler"} {
		c, ok := reg.Lookup(n)
		if !ok {
			t.Fatalf("corpus has no %q", n)
		}
		if u := reg.Unsupported(c, sup); len(u) > 0 {
			t.Errorf("%s: unsupported %v, want none after the repl:Draw registration", n, u)
		}
	}
}

// drawReplEngine is a two-seat game at Main 1 of turn 1 (seat 0 active), the
// board every Draw-replacement pin reads: NotFirstCardInDrawStep$ is
// false there, so a raw Draw is an "extra" draw the class may replace.
func drawReplEngine(t *testing.T, seed uint64) *Engine {
	t.Helper()
	return stealEngine(t, seed)
}

// setDrawHand replaces seat p's hand with n freshly added Mountain objects,
// returning the resulting hand size. The Draw replacement's condition gates
// count this hand, so the precondition is asserted by the caller.
func setDrawHand(t *testing.T, e *Engine, p state.PlayerID, n int) {
	t.Helper()
	e.G.SetZone(state.ZHand, p, nil)
	ids := make([]state.ObjID, 0, n)
	for i := 0; i < n; i++ {
		o := e.G.AddObject(mustCorpusCard(t, testutil.CorpusRegistry(t), "Mountain"), p)
		o.Zone = state.ZHand
		ids = append(ids, o.ID)
	}
	e.G.SetZone(state.ZHand, p, ids)
}

// TestBardKingOfDaleDoublesAnExtraDraw pins the "draw two cards instead"
// body: a non-first draw by Bard's controller becomes two. The turn-based
// draw exemption (NotFirstCardInDrawStep$ True) is covered by the Notion
// Thief pins in draw_replacement_test.go; this is the registration's own
// behavioural leaf.
func TestBardKingOfDaleDoublesAnExtraDraw(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := drawReplEngine(t, 743)
	onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Bard, King of Dale"))
	setDrawHand(t, e, 0, 0)
	if got := len(e.G.Zone(state.ZHand, 0)); got != 0 {
		t.Fatalf("precondition: seat 0 hand = %d, want 0", got)
	}

	before := countDraw(e)
	emitDraw(t, e, 0)
	if got := countDraw(e) - before; got != 2 {
		t.Fatalf("Draw events for one replaced draw = %d, want 2 (Bard draws two instead)", got)
	}
	if got := len(e.G.Zone(state.ZHand, 0)); got != 2 {
		t.Fatalf("seat 0 hand after replacement = %d, want 2", got)
	}
}

// TestLivingConundrumSkipsTheDrawOnAnEmptyLibrary pins the Prevent$ True
// (bodyless) arm gated by IsPresent$/PresentZone$/PresentCompare$: with an
// empty library the draw is swallowed whole, with a card in library it
// happens untouched.
func TestLivingConundrumSkipsTheDrawOnAnEmptyLibrary(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := drawReplEngine(t, 744)
	onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Living Conundrum"))
	setDrawHand(t, e, 0, 0)
	e.G.SetZone(state.ZLibrary, 0, nil)
	if got := len(e.G.Zone(state.ZLibrary, 0)); got != 0 {
		t.Fatalf("precondition: seat 0 library = %d, want 0", got)
	}

	before := countDraw(e)
	emitDrawExpectNone(t, e, 0)
	if got := countDraw(e) - before; got != 0 {
		t.Fatalf("Draw events with an empty library = %d, want 0 (Living Conundrum prevents it)", got)
	}

	// The over-reach guard: with a card in library the gate fails and the
	// draw proceeds.
	setupDrawLibrary(t, e, 0, mustCorpusCard(t, reg, "Mountain"))
	before = countDraw(e)
	emitDraw(t, e, 0)
	if got := countDraw(e) - before; got != 1 {
		t.Fatalf("Draw events with a stocked library = %d, want 1 (gate must not stick)", got)
	}
}

// emitDrawExpectNone proposes a Draw for p without requiring a card in p's
// library. emitDraw's own t.Fatal ("empty library for the test draw") would
// hide the very state this pin tests.
func emitDrawExpectNone(t *testing.T, e *Engine, p state.PlayerID) {
	t.Helper()
	e.emit(events.Event{Kind: events.Draw, Player: p, From: state.ZLibrary, To: state.ZHand, Secret: true})
}

// TestQuantumRiddlerDrawsOneExtraAtOneOrFewerCards pins the DrawCards alias
// plus the CheckSVar$/SVarCompare$ condition gate: with one card in hand the
// draw becomes "that many plus one"; with more it is unchanged.
func TestQuantumRiddlerDrawsOneExtraAtOneOrFewerCards(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := drawReplEngine(t, 745)
	onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Quantum Riddler"))
	setDrawHand(t, e, 0, 1)
	if got := len(e.G.Zone(state.ZHand, 0)); got != 1 {
		t.Fatalf("precondition: seat 0 hand = %d, want 1 (the LE1 gate must bite)", got)
	}

	before := countDraw(e)
	emitDraw(t, e, 0)
	if got := countDraw(e) - before; got != 2 {
		t.Fatalf("Draw events at one card in hand = %d, want 2 (plus one)", got)
	}

	// The gate's other side: more than one card in hand leaves the draw alone.
	setDrawHand(t, e, 0, 3)
	setupDrawLibrary(t, e, 0, mustCorpusCard(t, reg, "Mountain"))
	before = countDraw(e)
	emitDraw(t, e, 0)
	if got := countDraw(e) - before; got != 1 {
		t.Fatalf("Draw events at three cards in hand = %d, want 1 (gate must not apply)", got)
	}
}
