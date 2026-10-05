package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// Class level-up semantics are exact, not incremental (CR 716.2b, 716.2d):
// a level ability is legal only while the Class is at exactly that level
// minus one, and resolving it sets the designation to the level indicated.
// This is the shape a scratch probe caught on the OLD counter model: at level
// 1 the gate was `counters_LT3_LEVEL`, so the LEVEL-3 activator was offered at
// level 1, and activating it produced level 2 rather than level 3.
//
// Artist's Talent (BLB) is the carrier: two K:Class lines, level 2 ({2}{R})
// and level 3 ({2}{R}).
func TestClassLevelActivatorIsExactNMinusOne(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, _ := classEngine(t, reg, "Artist's Talent")
	id := classMove(t, e, "Artist's Talent", state.ZBattlefield)
	o := e.G.Obj(id)
	if o == nil || o.Zone != state.ZBattlefield {
		t.Fatal("precondition: Class must be on the battlefield")
	}
	if got := o.ClassLevel(); got != 1 {
		t.Fatalf("precondition: entry level = %d, want 1", got)
	}
	// Both level activators exist on the face; the gate is what withholds the
	// higher one. Index 0 is the level-2 line, index 1 the level-3 line.
	if !classHasAbility(e, id, 0) || !classHasAbility(e, id, 1) {
		t.Fatal("precondition: both level-up activators must exist on the face")
	}

	// At level 1 only the level-2 activator (gate classLevel_EQ1) is legal;
	// the level-3 activator (gate classLevel_EQ2) must NOT be offered.
	addMana(t, e, 0, "RRRRRR")
	if _, ok := findAbilityOption(e, id, 0); !ok {
		t.Fatalf("level-2 activator not offered at level 1: %+v", e.Pending().Options)
	}
	if _, ok := findAbilityOption(e, id, 1); ok {
		t.Fatalf("level-3 activator offered at level 1 (the old LT<3> gate over-offered): %+v", e.Pending().Options)
	}

	// Activate level 2: the designation lands on 2.
	submitChoices(t, e, mustAbilityOption(t, e, id, 0).Index)
	passUntilStackEmpty(t, e, 20)
	if got := o.ClassLevel(); got != 2 {
		t.Fatalf("after level-2 activation level = %d, want 2", got)
	}

	// At level 2 the level-2 activator is withdrawn and the level-3 activator
	// (exactly N-1 = 2) is now the legal one; activating it sets level 3.
	e.Advance()
	if _, ok := findAbilityOption(e, id, 0); ok {
		t.Fatalf("level-2 activator still offered at level 2: %+v", e.Pending().Options)
	}
	addMana(t, e, 0, "RRRRRR")
	l3, ok := findAbilityOption(e, id, 1)
	if !ok {
		t.Fatalf("level-3 activator not offered at level 2: %+v", e.Pending().Options)
	}
	submitChoices(t, e, l3.Index)
	passUntilStackEmpty(t, e, 20)
	if got := o.ClassLevel(); got != 3 {
		t.Fatalf("after level-3 activation level = %d, want 3 (set-to-N, not +1)", got)
	}
}

// TestClassLevelUpEmitsTheFoldDelta pins the event the fold consumes: the
// level-up effect emits a ClassLevelChange whose Amount carries the
// designation to the activator's target N (delta N-current), never a bare 1.
func TestClassLevelUpEmitsTheFoldDelta(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, _ := classEngine(t, reg, "Artist's Talent")
	id := classMove(t, e, "Artist's Talent", state.ZBattlefield)
	if got := e.G.Obj(id).ClassLevel(); got != 1 {
		t.Fatalf("precondition: entry level = %d, want 1", got)
	}

	addMana(t, e, 0, "RRRRRR")
	submitChoices(t, e, mustAbilityOption(t, e, id, 0).Index)
	passUntilStackEmpty(t, e, 20)
	if got := e.G.Obj(id).ClassLevel(); got != 2 {
		t.Fatalf("level-2 activation landed at %d, want 2", got)
	}
	found := false
	for _, ev := range e.L.Events {
		if ev.Kind == events.ClassLevelChange && ev.Obj == id && ev.Amount == 1 {
			found = true
		}
	}
	if !found {
		t.Fatal("no ClassLevelChange delta emitted for the level-up")
	}
}

func mustAbilityOption(t *testing.T, e *Engine, id state.ObjID, idx int) decision.Option {
	t.Helper()
	opt, ok := findAbilityOption(e, id, idx)
	if !ok {
		t.Fatalf("ability option %d not offered: %+v", idx, e.Pending().Options)
	}
	return opt
}
