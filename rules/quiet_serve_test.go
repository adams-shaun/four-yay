package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// The quiet serve's tests (quiet-seat design
// docs/superpowers/specs/2026-10-08-quiet-seat-walk-skip-design.md §6 Q2
// "Done means"): a proved window's priorityOptions is the walk's list,
// byte-identically. Every test asserts the serve path RAN (quietServed
// moved) and the precondition its comparison rests on (the proof says
// quiet; the walk's list has the shape the comparison needs), so a reverted
// hook or a vacuous fixture fails loudly instead of passing silently.

// quietServeTestOn runs f with the quiet serve forced on. The flag is the
// package var the hook reads (the test binary sets no GORGE_QUIET_SKIP env,
// and the env is read once at init), so the test drives the var directly
// and restores it.
func quietServeTestOn(t *testing.T, f func(t *testing.T)) {
	t.Helper()
	old := quietOff
	quietOff = false
	defer func() { quietOff = old }()
	f(t)
}

// quietServeWalk is the reference list: the real priority walk for p, the
// one the serve must equal field by field (optionsEqual, Index included).
func quietServeWalk(e *Engine, p state.PlayerID) []decision.Option {
	return e.legalActionsWithWindow(p, nil)
}

// quietServeKindCounts returns how many options of each kind a list holds.
func quietServeKindCounts(t *testing.T, opts []decision.Option) (activate, pass, concede, other int) {
	t.Helper()
	for i := range opts {
		switch opts[i].Kind {
		case "activate":
			activate++
		case "pass":
			pass++
		case "concede":
			concede++
		default:
			other++
		}
	}
	return
}

// TestQuietServePlainsOnly: the §6 Q2 seed fixture. A Plains-only seat is
// proved quiet, the walk offers exactly its bare mana tap plus pass and
// concede, and priorityOptions serves that list identically without running
// the walk.
func TestQuietServePlainsOnly(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	quietServeTestOn(t, func(t *testing.T) {
		e := quietBase(t, reg)
		walk := quietServeWalk(e, 0)
		if !e.seatQuiet(0) {
			t.Fatalf("precondition: the Plains-only seat is not proved quiet (blocker %s)", quietBlockerNames[e.quietBlocker(0)])
		}
		if act, pass, con, other := quietServeKindCounts(t, walk); act != 1 || pass != 1 || con != 1 || other != 0 {
			t.Fatalf("precondition: the walk offered %d activate/%d pass/%d concede/%d other, want 1/1/1/0: %v",
				act, pass, con, other, optKinds(walk))
		}
		served0 := quietServedCount()
		got := e.priorityOptions(0, nil)
		if n := quietServedCount() - served0; n != 1 {
			t.Fatalf("the quiet serve did not run (served delta %d): the hook or its registration moved", n)
		}
		if !optionsEqual(got, walk) {
			t.Fatalf("served %v, the walk offered %v", optKinds(got), optKinds(walk))
		}
		for i := range got {
			if !optionEqual(&got[i], &walk[i]) {
				t.Fatalf("served option %d %+v, the walk offered %+v", i, got[i], walk[i])
			}
		}
	})
}

// TestQuietServeTappedOut: a seat whose only source is tapped is proved
// quiet and served pass+concede, exactly the walk's list.
func TestQuietServeTappedOut(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	quietServeTestOn(t, func(t *testing.T) {
		e := quietBase(t, reg)
		lands := e.G.Zone(state.ZBattlefield, 0)
		if len(lands) != 1 {
			t.Fatalf("precondition: seat 0 holds %d battlefield permanents, want 1", len(lands))
		}
		e.emit(events.Event{Kind: events.Tap, Obj: lands[0]})
		if o := e.G.Obj(lands[0]); o == nil || !o.Tapped {
			t.Fatalf("precondition: the Plains did not tap: %+v", o)
		}
		walk := quietServeWalk(e, 0)
		if !e.seatQuiet(0) {
			t.Fatalf("precondition: the tapped-out seat is not proved quiet (blocker %s)", quietBlockerNames[e.quietBlocker(0)])
		}
		if act, pass, con, other := quietServeKindCounts(t, walk); act != 0 || pass != 1 || con != 1 || other != 0 {
			t.Fatalf("precondition: the walk offered %d activate/%d pass/%d concede/%d other, want 0/1/1/0: %v",
				act, pass, con, other, optKinds(walk))
		}
		served0 := quietServedCount()
		got := e.priorityOptions(0, nil)
		if n := quietServedCount() - served0; n != 1 {
			t.Fatalf("the quiet serve did not run (served delta %d)", n)
		}
		if !optionsEqual(got, walk) {
			t.Fatalf("served %v, the walk offered %v", optKinds(got), optKinds(walk))
		}
	})
}

// TestQuietServeInertHeldOut: a mana activate the inert backstop is holding
// out is filtered from the served list exactly as from the walk's (the tail
// filter runs on both; quietUsable deliberately does not require an empty
// hold-out, design §3.2).
func TestQuietServeInertHeldOut(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	quietServeTestOn(t, func(t *testing.T) {
		e := quietBase(t, reg)
		full := quietServeWalk(e, 0)
		if act, _, _, other := quietServeKindCounts(t, full); act != 1 || other != 0 {
			t.Fatalf("precondition: the unfiltered walk offered %d activate/%d other, want 1/0: %v",
				act, other, optKinds(full))
		}
		var manaOpt decision.Option
		for _, o := range full {
			if o.Kind == "activate" {
				manaOpt = o
				break
			}
		}
		e.inertHeldOut = map[inertKey]bool{}
		e.inertHeldOut[inertKeyOf(manaOpt)] = true
		if !e.seatQuiet(0) {
			t.Fatalf("precondition: the seat is not proved quiet with the hold-out set (blocker %s)", quietBlockerNames[e.quietBlocker(0)])
		}
		walk := quietServeWalk(e, 0)
		if act, _, _, _ := quietServeKindCounts(t, walk); act != 0 {
			t.Fatalf("precondition: the hold-out did not filter the walk's mana activate: %v", optKinds(walk))
		}
		served0 := quietServedCount()
		got := e.priorityOptions(0, nil)
		if n := quietServedCount() - served0; n != 1 {
			t.Fatalf("the quiet serve did not run (served delta %d)", n)
		}
		if !optionsEqual(got, walk) {
			t.Fatalf("served %v, the walk offered %v", optKinds(got), optKinds(walk))
		}
	})
}

// TestQuietServeSplitSecond: with a split-second spell on the stack the walk
// keeps the mana section (mana abilities stay legal, CR 702.62) and the
// serve's tail runs the same filter chain, so the served list is the walk's.
func TestQuietServeSplitSecond(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	quietServeTestOn(t, func(t *testing.T) {
		e := quietBaseWith(t, reg, []*cards.Card{lookup(t, reg, "Krosan Grip")})
		addZone(t, e, 0, lookup(t, reg, "Krosan Grip"), state.ZStack)
		if !e.splitSecondHolds() {
			t.Fatal("precondition: split second does not hold with Krosan Grip on the stack")
		}
		if !e.seatQuiet(0) {
			t.Fatalf("precondition: the seat is not proved quiet (blocker %s)", quietBlockerNames[e.quietBlocker(0)])
		}
		walk := quietServeWalk(e, 0)
		if act, pass, con, other := quietServeKindCounts(t, walk); act != 1 || pass != 1 || con != 1 || other != 0 {
			t.Fatalf("precondition: the walk offered %d activate/%d pass/%d concede/%d other, want 1/1/1/0 (mana abilities stay legal): %v",
				act, pass, con, other, optKinds(walk))
		}
		served0 := quietServedCount()
		got := e.priorityOptions(0, nil)
		if n := quietServedCount() - served0; n != 1 {
			t.Fatalf("the quiet serve did not run (served delta %d)", n)
		}
		if !optionsEqual(got, walk) {
			t.Fatalf("served %v, the walk offered %v", optKinds(got), optKinds(walk))
		}
	})
}

// TestQuietServeRecorderArmedBypass: on a recorder-armed engine
// (potentialFullDemand && WalkRecDemand) the serve is bypassed and the walk
// runs, because the record feeds PotentialMana's membership lists
// (design §6 Q2 "out of scope"). The proof still says quiet -- the bypass is
// quietUsable, not the proof.
func TestQuietServeRecorderArmedBypass(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	quietServeTestOn(t, func(t *testing.T) {
		e := quietBase(t, reg)
		e.potentialFullDemand = true
		e.WalkRecDemand = true
		if e.quietUsable() {
			t.Fatal("precondition: quietUsable is true on a recorder-armed engine")
		}
		if !e.seatQuiet(0) {
			t.Fatalf("precondition: the seat is not proved quiet (blocker %s)", quietBlockerNames[e.quietBlocker(0)])
		}
		walk0 := e.legalActionWalks
		served0 := quietServedCount()
		got := e.priorityOptions(0, nil)
		if n := quietServedCount() - served0; n != 0 {
			t.Fatalf("the quiet serve ran on a recorder-armed engine (served delta %d)", n)
		}
		if n := e.legalActionWalks - walk0; n != 1 {
			t.Fatalf("the walk did not run on the bypass (walk delta %d, want 1)", n)
		}
		walk := quietServeWalk(e, 0)
		if !optionsEqual(got, walk) {
			t.Fatalf("bypass result %v, the walk offered %v", optKinds(got), optKinds(walk))
		}
	})
}

// TestQuietServeFlagOff: with quietOff set the hook is dead and the walk
// runs, whatever the proof says. The flag is the package var the env seed
// (GORGE_QUIET_SKIP) drives at init.
func TestQuietServeFlagOff(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	old := quietOff
	quietOff = true
	defer func() { quietOff = old }()
	e := quietBase(t, reg)
	if !e.seatQuiet(0) {
		t.Fatalf("precondition: the seat is not proved quiet (blocker %s)", quietBlockerNames[e.quietBlocker(0)])
	}
	walk0 := e.legalActionWalks
	served0 := quietServedCount()
	got := e.priorityOptions(0, nil)
	if n := quietServedCount() - served0; n != 0 {
		t.Fatalf("the quiet serve ran while quietOff was set (served delta %d)", n)
	}
	if n := e.legalActionWalks - walk0; n < 1 {
		t.Fatalf("the walk did not run while quietOff was set (walk delta %d)", n)
	}
	walk := quietServeWalk(e, 0)
	if !optionsEqual(got, walk) {
		t.Fatalf("flagged-off result %v, the walk offered %v", optKinds(got), optKinds(walk))
	}
}

// TestQuietServeWindowCollectorSkipped: the hook requires window == nil; an
// ask carrying a diagnostics collector always walks (the collector classifies
// what the walk withheld, which a serve cannot answer).
func TestQuietServeWindowCollectorSkipped(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	quietServeTestOn(t, func(t *testing.T) {
		e := quietBase(t, reg)
		if !e.seatQuiet(0) {
			t.Fatalf("precondition: the seat is not proved quiet (blocker %s)", quietBlockerNames[e.quietBlocker(0)])
		}
		served0 := quietServedCount()
		var w windowCollector
		e.priorityOptions(0, &w)
		if n := quietServedCount() - served0; n != 0 {
			t.Fatalf("the quiet serve ran under a diagnostics collector (served delta %d)", n)
		}
	})
}
