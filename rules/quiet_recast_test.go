package rules

// Q3a: the graveyard/exile recast mana floor (design
// docs/superpowers/specs/2026-10-08-quiet-seat-walk-skip-design.md §6 Q3a).
//
// The Q1 proof blocked on the raw recastKW head, so ANY flashback card in the
// graveyard -- affordable or not -- made the window unprovable. Q3a prices the
// route per face: a flashback whose cost is above the seat's mana ceiling is
// quiet, one at the ceiling blocks, a sorcery-speed flashback outside a main
// phase is quiet while an instant-speed one blocks, and a route with a
// non-mana part (Escape's exile) stays blocked.
//
// Every row asserts the route is real (the walk offers the recast) where the
// row's claim is about affordability, so a fixture whose card never reached
// the graveyard or never carried the keyword cannot pass vacuously.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// quietBasePool is quietBaseWith with seat 0's battlefield emptied and its
// floating pool set to `units` blue mana, so the proof's mana ceiling is
// exactly `units` (the walk's castability gate prices the floating pool, not
// untapped lands -- see TestQuietDerivedGraveRouteBlocksGrantedFlashback).
func quietBasePool(t *testing.T, reg *cards.Registry, units int32, extras []*cards.Card) *Engine {
	t.Helper()
	e := quietBaseWith(t, reg, extras)
	for _, id := range append([]state.ObjID{}, e.G.Zone(state.ZBattlefield, 0)...) {
		e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZBattlefield, To: state.ZLibrary})
	}
	if units > 0 {
		e.G.Players[0].Pool[state.MU] = units
	}
	ceiling, unbounded, restricted := e.quietManaCeiling(0)
	if restricted || unbounded || ceiling != units {
		t.Fatalf("precondition: mana ceiling is %d (unbounded=%v restricted=%v), want %d", ceiling, unbounded, restricted, units)
	}
	return e
}

// quietFlashbackInstant is a {1}{U} instant with flashback {2}{U} (floor 3).
func quietFlashbackInstant(t testing.TB) *cards.Card {
	return card(t, "Name:Quiet Recast Instant\nManaCost:1 U\nTypes:Instant\n"+
		"K:Flashback:2 U\nA:SP$ Draw | NumCards$ 1\nOracle:x\n")
}

// quietFlashbackSorcery is a {1}{U} sorcery with flashback {1}{U} (floor 2).
func quietFlashbackSorcery(t testing.TB) *cards.Card {
	return card(t, "Name:Quiet Recast Sorcery\nManaCost:1 U\nTypes:Sorcery\n"+
		"K:Flashback:1 U\nA:SP$ Draw | NumCards$ 1\nOracle:x\n")
}

// quietEscapeSorcery is a {U} sorcery with escape {2}{U} plus an
// ExileFromGrave<2/Card.Other> part, so the route is open (non-mana).
func quietEscapeSorcery(t testing.TB) *cards.Card {
	return card(t, "Name:Quiet Recast Escape\nManaCost:U\nTypes:Sorcery\n"+
		"K:Escape:2 U ExileFromGrave<2/Card.Other/other>\nA:SP$ Draw | NumCards$ 1\nOracle:x\n")
}

// TestQuietRecastFlashbackFloor is the Q3a core: a flashback cost above the
// ceiling is quiet; at the ceiling it blocks.
func TestQuietRecastFlashbackFloor(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	t.Run("above the ceiling is quiet", func(t *testing.T) {
		fb := quietFlashbackInstant(t)
		// Floor 3, ceiling 2: the walk cannot pay {2}{U}.
		e := quietBasePool(t, reg, 2, []*cards.Card{fb})
		id := addZone(t, e, 0, fb, state.ZGraveyard)
		ff := e.walkFaceFactsOf(e.G.Obj(id).Face())
		if ff == nil || ff.quiet.recastFloor != 3 || ff.quiet.recastOpen {
			t.Fatalf("precondition: flashback facts are %+v, want floor 3 open=false", ff)
		}
		ceiling, _, _ := e.quietManaCeiling(0)
		if ceiling >= ff.quiet.recastFloor {
			t.Fatalf("precondition: ceiling %d is not below the floor %d", ceiling, ff.quiet.recastFloor)
		}
		// The route is real: the same board with one more unit of mana offers
		// the flashback cast. This is the control that the unaffordable board
		// differs only by the ceiling, not by a missing route.
		e2 := quietBasePool(t, reg, 3, []*cards.Card{quietFlashbackInstant(t)})
		fb2 := pullCardByName(t, e2, "Quiet Recast Instant")
		addZone(t, e2, 0, fb2, state.ZGraveyard)
		e2.priorityRound()
		if !hasMode(e2.legalActions(0), "flashback") {
			t.Fatalf("control: the walk does not offer the affordable flashback: %v", optKinds(e2.legalActions(0)))
		}
		if got := e.quietBlocker(0); got != qbNone {
			t.Fatalf("quietBlocker = %s, want %s (a flashback above the ceiling is quiet)",
				quietBlockerNames[got], quietBlockerNames[qbNone])
		}
		if !e.seatQuiet(0) {
			t.Fatal("the proof blocked a window whose flashback the seat cannot afford")
		}
	})
	t.Run("at the ceiling blocks", func(t *testing.T) {
		fb := quietFlashbackInstant(t)
		// Floor 3, ceiling 3: the walk can pay {2}{U}.
		e := quietBasePool(t, reg, 3, []*cards.Card{fb})
		id := addZone(t, e, 0, fb, state.ZGraveyard)
		ff := e.walkFaceFactsOf(e.G.Obj(id).Face())
		if ff == nil || ff.quiet.recastFloor != 3 || ff.quiet.recastOpen {
			t.Fatalf("precondition: flashback facts are %+v, want floor 3 open=false", ff)
		}
		e.priorityRound()
		if !hasMode(e.legalActions(0), "flashback") {
			t.Fatalf("precondition: the walk does not offer the affordable flashback: %v", optKinds(e.legalActions(0)))
		}
		if got := e.quietBlocker(0); got != qbGraveRoute {
			t.Fatalf("quietBlocker = %s, want %s (a flashback at the ceiling blocks)",
				quietBlockerNames[got], quietBlockerNames[qbGraveRoute])
		}
	})
}

// TestQuietRecastFlashbackTiming: outside a main phase an instant-speed
// flashback still blocks while a sorcery-speed one is quiet (its timing is
// closed).
func TestQuietRecastFlashbackTiming(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	t.Run("instant-speed outside a main phase blocks", func(t *testing.T) {
		fb := quietFlashbackInstant(t)
		e := quietBasePool(t, reg, 3, []*cards.Card{fb})
		addZone(t, e, 0, fb, state.ZGraveyard)
		driveToStep(t, e, e.G.Turn, 0, state.StepBeginCombat)
		if e.G.Step.IsMain() {
			t.Fatal("precondition: still at a main step")
		}
		// The step change emptied the floating pool (CR 500.4); refill it so
		// the instant's flashback is affordable at the non-sorcery window.
		e.G.Players[0].Pool[state.MU] = 3
		e.priorityRound()
		if !hasMode(e.legalActions(0), "flashback") {
			t.Fatalf("precondition: the walk does not offer the instant flashback outside a main phase: %v", optKinds(e.legalActions(0)))
		}
		if got := e.quietBlocker(0); got != qbGraveRoute {
			t.Fatalf("quietBlocker = %s, want %s (an instant flashback outside a main phase blocks)",
				quietBlockerNames[got], quietBlockerNames[qbGraveRoute])
		}
	})
	t.Run("sorcery-speed outside a main phase is quiet", func(t *testing.T) {
		fb := quietFlashbackSorcery(t)
		e := quietBasePool(t, reg, 2, []*cards.Card{fb})
		addZone(t, e, 0, fb, state.ZGraveyard)
		driveToStep(t, e, e.G.Turn, 0, state.StepBeginCombat)
		if e.G.Step.IsMain() {
			t.Fatal("precondition: still at a main step")
		}
		// The route is real but timing-gated: outside a main phase the walk
		// offers no sorcery-speed flashback, so the quiet verdict is not a
		// missing route.
		if hasMode(e.legalActions(0), "flashback") {
			t.Fatalf("control: the walk offers a sorcery-speed flashback outside a main phase: %v", optKinds(e.legalActions(0)))
		}
		if got := e.quietBlocker(0); got != qbNone {
			t.Fatalf("quietBlocker = %s, want %s (a sorcery flashback outside a main phase is quiet)",
				quietBlockerNames[got], quietBlockerNames[qbNone])
		}
	})
}

// TestQuietRecastEscapeOpen: Escape's exile part makes the route open, so an
// affordable escape in the graveyard still blocks -- and the walk really does
// offer it, so the row is not vacuous.
func TestQuietRecastEscapeOpen(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	esc := quietEscapeSorcery(t)
	bears := lookup(t, reg, "Grizzly Bears")
	// Three blue in the pool pay {2}{U}; two Grizzly Bears in the graveyard
	// are the two OTHER cards the escape exile part needs.
	e := quietBasePool(t, reg, 3, []*cards.Card{esc, bears, bears})
	id := addZone(t, e, 0, esc, state.ZGraveyard)
	addZone(t, e, 0, bears, state.ZGraveyard)
	addZone(t, e, 0, bears, state.ZGraveyard)
	ff := e.walkFaceFactsOf(e.G.Obj(id).Face())
	if ff == nil || !ff.quiet.recastOpen {
		t.Fatalf("precondition: escape facts are %+v, want recastOpen", ff)
	}
	e.priorityRound()
	if !hasMode(e.legalActions(0), "escape") {
		t.Fatalf("precondition: the walk does not offer the affordable escape: %v", optKinds(e.legalActions(0)))
	}
	if got := e.quietBlocker(0); got != qbGraveRoute {
		t.Fatalf("quietBlocker = %s, want %s (an escape route is open and blocks)",
			quietBlockerNames[got], quietBlockerNames[qbGraveRoute])
	}
}

// pullCardByName returns the *cards.Card of the named face in seat 0's
// library (a fresh compile the registry does not hold).
func pullCardByName(t *testing.T, e *Engine, name string) *cards.Card {
	t.Helper()
	for _, id := range e.G.Zone(state.ZLibrary, 0) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == name {
			return o.Card
		}
	}
	t.Fatalf("precondition: %q not in seat 0's library", name)
	return nil
}
