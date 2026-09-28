package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// This file is the regression suite for the choice-free Mill<N> component in
// the generic mid-resolution UnlessCost$ path (Deep Spawn's "sacrifice
// CARDNAME unless you mill two cards"). Before it, ParseUnlessCost rejected
// the token, the unless-pay arm read that as a hard decline, and the creature
// was always sacrificed even when the payer chose Pay with a stocked library.

// TestParseUnlessCostMill pins the strict parser boundary the pay path
// depends on: a fixed Mill<N> is accepted, and every dynamic or malformed
// spelling still fails closed.
func TestParseUnlessCostMill(t *testing.T) {
	t.Parallel()
	cost, ok := ParseUnlessCost("Mill<2>")
	if !ok {
		t.Fatalf("ParseUnlessCost(Mill<2>) = !ok, want the fixed mill cost accepted")
	}
	if len(cost.Mill) != 1 || cost.Mill[0].N != 2 {
		t.Fatalf("Mill<2> parsed to %+v, want one Mill part of 2", cost.Mill)
	}
	// A composed cost keeps the Mill part alongside mana.
	composed, ok := ParseUnlessCost("1 U Mill<3>")
	if !ok {
		t.Fatalf("ParseUnlessCost(1 U Mill<3>) = !ok, want accepted")
	}
	if len(composed.Mill) != 1 || composed.Mill[0].N != 3 || composed.Generic != 1 {
		t.Fatalf("composed cost parsed to %+v (generic %d), want generic 1 + Mill<3>", composed.Mill, composed.Generic)
	}
	// The malformed/dynamic shapes stay strict failures, so an unpriceable
	// Mill can never buy itself for free or silently drop to zero.
	for _, bad := range []string{"Mill<X>", "Mill<>", "Mill<2", "Mill 2", "Mill<-1>", "Mill<2/you>"} {
		if _, ok := ParseUnlessCost(bad); ok {
			t.Errorf("ParseUnlessCost(%q) = ok, want a strict failure", bad)
		}
	}
}

// deepSpawnEngine seats the REAL compiled Deep Spawn on seat 0's battlefield
// in a two-seat game and stocks seat 0's library with n anonymous cards. The
// trigger fires at seat 0's own upkeep (ValidPlayer$ You).
func deepSpawnEngine(t *testing.T, libSize int) (*Engine, state.ObjID) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	e := New(Config{Seed: 1, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{mountainDeck(t, 40), mountainDeck(t, 40)}})
	for p := state.PlayerID(0); p < 2; p++ {
		e.G.SetZone(state.ZHand, p, nil)
	}
	o := e.G.AddObject(mustCorpusCard(t, reg, "Deep Spawn"), 0)
	o.Zone = state.ZBattlefield
	e.G.SetZone(state.ZBattlefield, 0, append(e.G.Zone(state.ZBattlefield, 0), o.ID))
	var lib []state.ObjID
	for i := 0; i < libSize; i++ {
		fodder := e.G.AddObject(millFodderCard(t), 0)
		lib = append(lib, fodder.ID)
	}
	e.G.SetZone(state.ZLibrary, 0, lib)
	return e, o.ID
}

// driveToUpkeepUnlessPay walks into seat 0's upkeep and returns the
// unless_pay KModes ask for the Deep Spawn sacrifice trigger.
func driveToUpkeepUnlessPay(t *testing.T, e *Engine) *decision.Decision {
	t.Helper()
	e.Advance()
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 2})
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepUpkeep})
	for i := 0; i < 100; i++ {
		d := e.Pending()
		if d == nil {
			t.Fatal("engine stalled with no pending decision before the upkeep ask")
		}
		if d.Kind == decision.KModes && d.ResumeKind == "unless_pay" && d.ResumeSA != nil && d.ResumeSA.API == "Sacrifice" {
			return d
		}
		if d.Kind != decision.KPriority {
			t.Fatalf("unexpected %v before the upkeep ask: %+v", d.Kind, d)
		}
		castFirst(t, e, "pass")
	}
	t.Fatal("no Sacrifice unless-pay ask posed by the Deep Spawn upkeep")
	return nil
}

// milledToGraveyard returns the ids of the supplied library snapshot that
// moved library -> graveyard, in event order.
func milledToGraveyard(e *Engine, lib []state.ObjID) []state.ObjID {
	moved := map[state.ObjID]bool{}
	for _, ev := range e.L.Events {
		if events.IsMill(ev) {
			moved[ev.Obj] = true
		}
	}
	var out []state.ObjID
	for _, id := range lib {
		if moved[id] {
			out = append(out, id)
		}
	}
	return out
}

// TestDeepSpawnUnlessMillCost is the end-to-end regression: with a stocked
// library the Pay option is genuinely offered, paying mills the top two cards
// through real MoveZone events, and the 6/6 stays on the battlefield instead
// of being sacrificed.
func TestDeepSpawnUnlessMillCost(t *testing.T) {
	t.Parallel()
	e, spawn := deepSpawnEngine(t, 5)
	lib := append([]state.ObjID(nil), e.G.Zone(state.ZLibrary, 0)...)
	if len(lib) != 5 {
		t.Fatalf("precondition: library has %d cards, want 5", len(lib))
	}
	if z := e.G.Obj(spawn).Zone; z != state.ZBattlefield {
		t.Fatalf("precondition: Deep Spawn zone = %s, want Battlefield", z)
	}

	ask := driveToUpkeepUnlessPay(t, e)
	// Precondition: the pay option and the decline option actually differ,
	// so selecting Pay is a real election rather than a single-option ask.
	if len(ask.Options) != 2 || ask.Options[0].Kind != "mode" || ask.Options[1].Kind != "mode" {
		t.Fatalf("expected a 2-option Pay/Don't pay ask, got %+v", ask.Options)
	}
	if ask.Options[0].Label == ask.Options[1].Label {
		t.Fatalf("pay and decline options are indistinguishable: %+v", ask.Options)
	}
	if ask.Player != 0 {
		t.Fatalf("ask player = seat %d, want the controller (UnlessPayer$ You)", ask.Player)
	}

	// Pay: the top two library cards mill to the graveyard and Deep Spawn
	// survives.
	submitChoices(t, e, ask.Options[0].Index)
	if moved := milledToGraveyard(e, lib); len(moved) != 2 || moved[0] != lib[0] || moved[1] != lib[1] {
		t.Fatalf("milled %v, want the top two library cards %v in order", moved, lib[:2])
	}
	if z := e.G.Obj(spawn).Zone; z != state.ZBattlefield {
		t.Fatalf("paid Deep Spawn zone = %s, want Battlefield (the mill spared it)", z)
	}
	if z := e.G.Obj(lib[0]).Zone; z != state.ZGraveyard {
		t.Fatalf("top library card zone = %s, want Graveyard", z)
	}
}

// TestDeepSpawnUnlessMillCostShortLibrary pins CR 701.13a's partial rule for
// the unless path: a library of one still pays Mill<2> by milling the one
// card available, so the cost is not a hard decline and Deep Spawn survives.
func TestDeepSpawnUnlessMillCostShortLibrary(t *testing.T) {
	t.Parallel()
	e, spawn := deepSpawnEngine(t, 1)
	lib := append([]state.ObjID(nil), e.G.Zone(state.ZLibrary, 0)...)
	if len(lib) != 1 {
		t.Fatalf("precondition: library has %d cards, want 1", len(lib))
	}

	ask := driveToUpkeepUnlessPay(t, e)
	if len(ask.Options) != 2 {
		t.Fatalf("short library exposed %d options, want the full Pay/Don't pay pair", len(ask.Options))
	}
	submitChoices(t, e, ask.Options[0].Index)
	if moved := milledToGraveyard(e, lib); len(moved) != 1 {
		t.Fatalf("short-library pay milled %v, want the one available card", moved)
	}
	if z := e.G.Obj(spawn).Zone; z != state.ZBattlefield {
		t.Fatalf("paid Deep Spawn zone = %s with a short library, want Battlefield", z)
	}
}

// TestDeepSpawnUnlessMillCostEmptyLibrary pins the zero-card half: milling
// zero still pays, so the cost is never a hard decline and Deep Spawn
// survives an empty library.
func TestDeepSpawnUnlessMillCostEmptyLibrary(t *testing.T) {
	t.Parallel()
	e, spawn := deepSpawnEngine(t, 0)
	if n := len(e.G.Zone(state.ZLibrary, 0)); n != 0 {
		t.Fatalf("precondition: library has %d cards, want 0", n)
	}

	ask := driveToUpkeepUnlessPay(t, e)
	if len(ask.Options) != 2 {
		t.Fatalf("empty library exposed %d options, want the full Pay/Don't pay pair", len(ask.Options))
	}
	submitChoices(t, e, ask.Options[0].Index)
	if z := e.G.Obj(spawn).Zone; z != state.ZBattlefield {
		t.Fatalf("paid Deep Spawn zone = %s with an empty library, want Battlefield", z)
	}
}
