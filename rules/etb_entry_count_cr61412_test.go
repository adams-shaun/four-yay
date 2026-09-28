package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

func findBattlefieldCard(t *testing.T, e *Engine, name string) state.ObjID {
	t.Helper()
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == name {
			return id
		}
	}
	t.Fatalf("%s is not on seat 0's battlefield", name)
	return 0
}

func castFromHand(t *testing.T, e *Engine, name, mana string) state.ObjID {
	t.Helper()
	id := searchMoveByName(t, e, name, state.ZHand)
	if id == 0 {
		t.Fatalf("%s is not in hand", name)
	}
	castVanilla(t, e, name, mana)
	return id
}

func TestGiadaEntryCountExcludesEnteringPermanentCR61412(t *testing.T) {
	reg := freshCorpusRegistry(t,
		"g/giada_font_of_hope.txt", "s/serra_angel.txt",
		"g/grizzly_bears.txt", "f/forest.txt", "m/mountain.txt", "p/plains.txt")

	t.Run("one existing Angel", func(t *testing.T) {
		e, _ := etbreplEngine(t, reg, "Giada, Font of Hope", "Serra Angel", "Grizzly Bears")
		castFromHand(t, e, "Giada, Font of Hope", "WW")
		giada := findBattlefieldCard(t, e, "Giada, Font of Hope")
		if got := counterCount(e.G.Obj(giada), "P1P1"); got != 0 {
			t.Fatalf("Giada's own-entry counters = %d, want 0", got)
		}

		angel := castFromHand(t, e, "Serra Angel", "WWWWW")
		if o := e.G.Obj(angel); o == nil || o.Zone != state.ZBattlefield {
			t.Fatalf("Serra Angel zone = %v, want battlefield", o)
		}
		if got := counterCount(e.G.Obj(angel), "P1P1"); got != 1 {
			t.Fatalf("Serra Angel counters = %d, want exactly 1 (Giada only)", got)
		}
		pt := e.Derived(angel)
		if pt.Power != 5 || pt.Toughness != 5 {
			t.Fatalf("Serra Angel P/T = %d/%d, want 5/5", pt.Power, pt.Toughness)
		}
	})

	t.Run("two existing Angels", func(t *testing.T) {
		e, _ := etbreplEngine(t, reg, "Giada, Font of Hope", "Serra Angel", "Serra Angel")
		castFromHand(t, e, "Giada, Font of Hope", "WW")
		giada := findBattlefieldCard(t, e, "Giada, Font of Hope")
		first := castFromHand(t, e, "Serra Angel", "WWWWW")
		if got := counterCount(e.G.Obj(first), "P1P1"); got != 1 {
			t.Fatalf("first Serra Angel counters = %d, want 1", got)
		}
		if e.G.Obj(giada).Zone != state.ZBattlefield || e.G.Obj(first).Zone != state.ZBattlefield {
			t.Fatal("precondition failed: Giada and first Serra Angel must be on battlefield before second Angel enters")
		}
		second := castFromHand(t, e, "Serra Angel", "WWWWW")
		if got := counterCount(e.G.Obj(second), "P1P1"); got != 2 {
			t.Fatalf("second Serra Angel counters = %d, want exactly 2", got)
		}
		pt := e.Derived(second)
		if pt.Power != 6 || pt.Toughness != 6 {
			t.Fatalf("second Serra Angel P/T = %d/%d, want 6/6", pt.Power, pt.Toughness)
		}
	})

	t.Run("non-Angel", func(t *testing.T) {
		e, _ := etbreplEngine(t, reg, "Giada, Font of Hope", "Grizzly Bears")
		castFromHand(t, e, "Giada, Font of Hope", "WW")
		findBattlefieldCard(t, e, "Giada, Font of Hope")
		bear := castFromHand(t, e, "Grizzly Bears", "GG")
		if e.G.Obj(bear).Zone != state.ZBattlefield {
			t.Fatal("Grizzly Bears did not enter the battlefield")
		}
		if got := counterCount(e.G.Obj(bear), "P1P1"); got != 0 {
			t.Fatalf("Grizzly Bears counters = %d, want 0", got)
		}
	})
}
