package rules

// Reference: Magic: The Gathering Comprehensive Rules, 2026-08-07.
// CR 614.1a: a replacement effect that says an event "can't happen" replaces
// that event with nothing -- the event simply does not occur.
//
// Bodyless (no ReplaceWith$) `R:Event$ Moved | Prevent$ True | Layer$
// CantHappen` replacement lines match their move but were dropped by the
// application path, so the move still happened. Five corpus cards carry the
// shape (Grafdigger's Cage, Kunoros Hound of Athreos, Soulless Jailer,
// Weathered Runestone, Worms of the Earth). These tests exercise the
// graveyard-origin half, which matches at base; the library-origin half of
// Grafdigger's Cage / Weathered Runestone is a separate defect.

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// graveyardOriginPreventRepl reports whether face carries a bodyless Moved
// replacement with Prevent$ True whose Origin$ names the graveyard, the exact
// compiled shape this fix keys on. It asserts the fixture is real.
func graveyardOriginPreventRepl(t *testing.T, e *Engine, id state.ObjID) bool {
	t.Helper()
	o := e.G.Obj(id)
	if o == nil || o.Face() == nil {
		t.Fatalf("prevent fixture source %d has no face", id)
	}
	for _, r := range o.Face().Repls {
		if r.Event != "Moved" || r.With != nil {
			continue
		}
		if !strings.EqualFold(r.Params["Prevent"], "True") || r.Params["Layer"] != "CantHappen" {
			continue
		}
		if r.Params["Origin"] == "Graveyard" || strings.Contains(r.Params["Origin"], "Graveyard") {
			return true
		}
	}
	return false
}

// TestCR614CantEnterBattlefieldPrevention proves CR 614.1a: with a bodyless
// Moved prevention on the battlefield, a creature card that would enter from a
// graveyard stays in the graveyard, while an ordinary entry (no prevention
// live) still lands. Raw emits, so no priority or cast flow is under test.
func TestCR614CantEnterBattlefieldPrevention(t *testing.T) {
	t.Parallel()
	e := crResolutionEngine(t, []string{"Grafdigger's Cage"}, nil)
	cage := crAbortMove(t, e, 0, "Grafdigger's Cage", state.ZBattlefield)
	bears := crAbortMove(t, e, 1, "Serra Avenger", state.ZGraveyard)

	// Precondition: the live cage really carries the bodyless prevent repl,
	// and the bears really sit in the graveyard the repl reads.
	if !graveyardOriginPreventRepl(t, e, cage) {
		t.Fatal("CR 614.1a fixture: Grafdigger's Cage has no bodyless graveyard-origin Prevent$ True Moved repl")
	}
	if e.G.Obj(bears).Zone != state.ZGraveyard {
		t.Fatalf("CR 614.1a fixture: bears zone=%s, want graveyard", e.G.Obj(bears).Zone)
	}
	if e.G.Obj(cage).Zone != state.ZBattlefield {
		t.Fatalf("CR 614.1a fixture: cage zone=%s, want battlefield", e.G.Obj(cage).Zone)
	}

	start := len(e.L.Events)
	e.pending = nil
	e.emit(events.Event{Kind: events.MoveZone, Obj: bears, From: state.ZGraveyard, To: state.ZBattlefield})
	if got := e.G.Obj(bears).Zone; got != state.ZGraveyard {
		t.Errorf("CR 614.1a Grafdigger's Cage seq %d: graveyard-origin entry landed, zone=%s; want graveyard (entry prevented)", start, got)
	}
}

// TestCR614CantEnterBattlefieldPreventionKunoros covers a second carrier
// (Kunoros, Hound of Athreos: Creature.Other, Origin$ Graveyard with no
// comma list) so the fix is not read as Grafdigger's-Cage-specific. Raw emit.
func TestCR614CantEnterBattlefieldPreventionKunoros(t *testing.T) {
	t.Parallel()
	e := crResolutionEngine(t, []string{"Kunoros, Hound of Athreos"}, nil)
	kunoros := crAbortMove(t, e, 0, "Kunoros, Hound of Athreos", state.ZBattlefield)
	bears := crAbortMove(t, e, 1, "Serra Avenger", state.ZGraveyard)

	if !graveyardOriginPreventRepl(t, e, kunoros) {
		t.Fatal("CR 614.1a fixture: Kunoros, Hound of Athreos has no bodyless graveyard-origin Prevent$ True Moved repl")
	}
	if e.G.Obj(bears).Zone != state.ZGraveyard || e.G.Obj(kunoros).Zone != state.ZBattlefield {
		t.Fatalf("CR 614.1a fixture: bears=%s kunoros=%s; want graveyard/battlefield", e.G.Obj(bears).Zone, e.G.Obj(kunoros).Zone)
	}

	e.pending = nil
	e.emit(events.Event{Kind: events.MoveZone, Obj: bears, From: state.ZGraveyard, To: state.ZBattlefield})
	if got := e.G.Obj(bears).Zone; got != state.ZGraveyard {
		t.Errorf("CR 614.1a Kunoros, Hound of Athreos: graveyard-origin entry landed, zone=%s; want graveyard (entry prevented)", got)
	}
}

// TestGrafdiggersCageCantEnterBattlefield is the replayable end-to-end
// fixture: Grafdigger's Cage sits on the battlefield, Grizzly Bears is put
// into a graveyard, and the graveyard->battlefield move is attempted. The
// move is prevented (CR 614.1a) and the log-only replay reproduces the same
// game exactly -- the prevented move leaves no event, so a replay that
// re-applied it would diverge.
func TestGrafdiggersCageCantEnterBattlefield(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := searchEngine(t, reg, "Grafdigger's Cage")
	cage := searchMoveByName(t, e, "Grafdigger's Cage", state.ZBattlefield)
	if !graveyardOriginPreventRepl(t, e, cage) {
		t.Fatal("fixture: Grafdigger's Cage has no bodyless graveyard-origin prevent repl")
	}
	if e.G.Obj(cage).Zone != state.ZBattlefield {
		t.Fatalf("fixture: cage zone=%s, want battlefield", e.G.Obj(cage).Zone)
	}

	bears := searchMoveByName(t, e, "Grizzly Bears", state.ZGraveyard)
	if e.G.Obj(bears).Zone != state.ZGraveyard {
		t.Fatalf("fixture: bears zone=%s, want graveyard", e.G.Obj(bears).Zone)
	}

	e.pending = nil
	e.emit(events.Event{Kind: events.MoveZone, Obj: bears, From: state.ZGraveyard, To: state.ZBattlefield})
	if got := e.G.Obj(bears).Zone; got != state.ZGraveyard {
		t.Errorf("CR 614.1a replayable fixture: bears landed on %s; want graveyard (entry prevented)", got)
	}
	// Also prove the cage itself is untouched: a card that stays in its zone
	// must not have been relocated by the prevention.
	if e.G.Obj(cage).Zone != state.ZBattlefield {
		t.Errorf("CR 614.1a replayable fixture: cage zone=%s, want battlefield", e.G.Obj(cage).Zone)
	}
	replayCheck(t, e, cfg)
}

// TestKunorosCantEnterBattlefield covers the third carrier (a creature with
// Origin$ Graveyard, no comma list) with a replayable fixture, proving the
// fix is not keyed to the cage's ValidLKI$ or its compound Origin$.
func TestKunorosCantEnterBattlefield(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := searchEngine(t, reg, "Kunoros, Hound of Athreos")
	kunoros := searchMoveByName(t, e, "Kunoros, Hound of Athreos", state.ZBattlefield)
	if !graveyardOriginPreventRepl(t, e, kunoros) {
		t.Fatal("fixture: Kunoros, Hound of Athreos has no bodyless graveyard-origin prevent repl")
	}
	if e.G.Obj(kunoros).Zone != state.ZBattlefield {
		t.Fatalf("fixture: Kunoros zone=%s, want battlefield", e.G.Obj(kunoros).Zone)
	}
	bears := searchMoveByName(t, e, "Grizzly Bears", state.ZGraveyard)
	if e.G.Obj(bears).Zone != state.ZGraveyard {
		t.Fatalf("fixture: bears zone=%s, want graveyard", e.G.Obj(bears).Zone)
	}
	e.pending = nil
	e.emit(events.Event{Kind: events.MoveZone, Obj: bears, From: state.ZGraveyard, To: state.ZBattlefield})
	if got := e.G.Obj(bears).Zone; got != state.ZGraveyard {
		t.Errorf("CR 614.1a Kunoros, Hound of Athreos: bears landed on %s; want graveyard (entry prevented)", got)
	}
	replayCheck(t, e, cfg)
}

// TestNoEntryWithoutPrevent is the control: the same graveyard->battlefield
// move with NO prevention live DOES land, so the tests above cannot pass
// vacuously when the move-emit itself is broken.
func TestNoEntryWithoutPrevent(t *testing.T) {
	t.Parallel()
	e := crResolutionEngine(t, nil, nil)
	bears := crAbortMove(t, e, 1, "Serra Avenger", state.ZGraveyard)
	if e.G.Obj(bears).Zone != state.ZGraveyard {
		t.Fatalf("control fixture: bears zone=%s, want graveyard", e.G.Obj(bears).Zone)
	}
	e.pending = nil
	e.emit(events.Event{Kind: events.MoveZone, Obj: bears, From: state.ZGraveyard, To: state.ZBattlefield})
	if got := e.G.Obj(bears).Zone; got != state.ZBattlefield {
		t.Errorf("control: unprevented graveyard-origin entry zone=%s; want battlefield", got)
	}
	testutil.CheckInvariants(t, e.G, e.Pending(), "unprevented entry")
}
