package rules

// Benthic Explorers' "Valid$ Defined.Untapped" reflected-mana selector binds
// to the ELECTED untapYType cost permanent (Ctx.CostUntapped) -- the tapped
// land an OPPONENT controls that the cost just untapped -- never to the
// resolving controller's own untapped permanents, and an empty binding stays
// fail-closed (no candidates -> the executor's Note). This file pins the
// binding at the candidates level and end to end, with a controller Plains
// on the board so the pre-fix widened scan would have answered W.

import (
	"testing"

	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func TestManaReflectedUntappedReflectsElectedNotController(t *testing.T) {
	t.Parallel()
	e, cfg, ids := manaTapBoard(t, 7810, "Benthic Explorers", "Plains")
	b, plains := ids["Benthic Explorers"], ids["Plains"]
	mnt := findByName(e, "Mountain", 1)
	if mnt == 0 {
		t.Fatal("fixture: seat 1 has no Mountain")
	}
	// Put one of seat 1's Mountains on its battlefield and tap it.
	e.emit(events.Event{Kind: events.MoveZone, Obj: mnt, From: state.ZLibrary, To: state.ZBattlefield})
	e.emit(events.Event{Kind: events.Tap, Obj: mnt})
	reprioritize(t, e)

	// Preconditions every assertion below depends on.
	if m := e.G.Obj(mnt); m == nil || m.Zone != state.ZBattlefield || m.Controller != 1 || !m.Tapped {
		t.Fatalf("fixture: opponent Mountain not a tapped seat-1 battlefield permanent (%+v)", m)
	}
	if p := e.G.Obj(plains); p == nil || p.Zone != state.ZBattlefield || p.Controller != 0 || p.Tapped {
		t.Fatalf("fixture: controller Plains not an untapped seat-0 battlefield permanent (%+v)", p)
	}
	svars := e.G.Obj(b).Face().SVars
	sa := e.G.Obj(b).Face().Abilities[0]
	if sa.API != "ManaReflected" {
		t.Fatalf("fixture: Benthic face ability 0 is %q, want ManaReflected", sa.API)
	}
	// The compared colours really differ: the Mountain reflects R...
	if got := effects.ManaReflectedCandidates(e, &effects.Ctx{Source: b, Controller: 0, SVars: svars,
		CostUntapped: []state.ObjID{mnt}}, sa); len(got) != 1 || got[0] != "R" {
		t.Fatalf("Mountain-bound reflection = %v, want [R]", got)
	}
	// ...and the Plains reflects W, so a W answer below really is the wrong
	// object, not the right object under another name.
	if got := effects.ManaReflectedCandidates(e, &effects.Ctx{Source: b, Controller: 0, SVars: svars,
		CostUntapped: []state.ObjID{plains}}, sa); len(got) != 1 || got[0] != "W" {
		t.Fatalf("Plains-bound reflection = %v, want [W]", got)
	}
	// Fail-closed: an empty binding reflects nothing at all -- the pre-fix
	// widened scan answered [W] here, from the controller's own Plains.
	if got := effects.ManaReflectedCandidates(e, &effects.Ctx{Source: b, Controller: 0, SVars: svars}, sa); len(got) != 0 {
		t.Fatalf("unbound reflection = %v, want none (fail-closed)", got)
	}

	// End to end: activating untaps the opponent Mountain and adds ITS red,
	// not the controller Plains' white.
	if !hasActivateOption(e, b) {
		t.Fatalf("Benthic Explorers not offered with a tapped opponent Mountain and an untapped controller Plains: %+v", e.Pending().Options)
	}
	submitChoices(t, e, activateOption(t, e, b))
	if o := e.G.Obj(mnt); o == nil || o.Tapped {
		t.Fatalf("elected opponent Mountain was not untapped as the cost: %+v", o)
	}
	if o := e.G.Obj(plains); o == nil || o.Tapped {
		t.Fatalf("the controller's bystander Plains was touched: %+v", o)
	}
	if got := e.G.Players[0].Pool[state.ManaIndex('R')]; got != 1 {
		t.Fatalf("reflected pool red slot = %d, want 1 from the elected Mountain", got)
	}
	if got := e.G.Players[0].Pool[state.ManaIndex('W')]; got != 0 {
		t.Fatalf("reflected pool white slot = %d, want 0 (the Plains is not the reflected object)", got)
	}
	replayCheck(t, e, cfg)
}
