package rules

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The rules test binary checks every printed fast-path answer against the
// full derivation (derived_printed.go). The package's testBot builds its
// board through botpolicy.BoardFromGameInto, so every fuzz and repo-deck game
// in the suite exercises the check.
func init() { printedCharsVerify = true }

// printedFast reports whether Characteristics would take the printed fast
// path for id right now (active() brought up to date first, as the board
// build's first derivation does).
func printedFast(e *Engine, id state.ObjID) bool {
	e.active()
	_, _, _, ok := e.printedCharacteristics(id, true)
	return ok
}

// TestPrintedCharacteristicsFastPath pins which objects the fast path
// answers and that its answer is the full derivation's: an untouched
// creature (counters and all) and a hand card take it while the only
// P/T/keyword effects are other objects' Card.Self effects; the source of a
// Card.Self effect does not; and once a lord's static can reach other
// objects, nothing does.
func TestPrintedCharacteristicsFastPath(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	bear := onBoard(t, e, 0, "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nK:Trample\nOracle:x\n")
	elk := onBoard(t, e, 0, "Name:Elk\nManaCost:1 G\nTypes:Creature Elk\nPT:3/3\nOracle:x\n")
	e.emit(events.Event{Kind: events.CounterChange, Obj: bear, Counter: "P1P1", Amount: 2})
	hand := e.G.Zone(state.ZHand, 0)
	if len(hand) == 0 {
		t.Fatal("no hand card")
	}
	inHand := hand[0]

	check := func(id state.ObjID, wantFast bool, wantP, wantT int32, wantKW []string) {
		t.Helper()
		if got := printedFast(e, id); got != wantFast {
			t.Fatalf("obj %d: fast path %v, want %v", id, got, wantFast)
		}
		p, tough, kw := e.Characteristics(id)
		d := e.Derived(id)
		if p != wantP || tough != wantT || !slices.Equal(kw, wantKW) {
			t.Fatalf("obj %d: Characteristics %d/%d %v, want %d/%d %v", id, p, tough, kw, wantP, wantT, wantKW)
		}
		if d.Power != p || d.Toughness != tough || !slices.Equal(d.Keywords, wantKW) {
			t.Fatalf("obj %d: Derived %d/%d %v disagrees with Characteristics", id, d.Power, d.Toughness, d.Keywords)
		}
	}
	check(bear, true, 4, 4, []string{"Trample"})
	check(elk, true, 3, 3, []string{})
	hp, ht, _ := e.Characteristics(inHand)
	if !printedFast(e, inHand) || hp != 0 || ht != 0 {
		t.Fatalf("hand land: fast %v, P/T %d/%d", printedFast(e, inHand), hp, ht)
	}

	// Keywords and ViewCharacteristics share the fast path; the keyword list
	// then aliases the face, never the derivation scratch.
	if kw, ok := e.printedKeywordsOnly(bear); !ok || !slices.Equal(kw, []string{"Trample"}) {
		t.Fatalf("Keywords fast path: %v %v", kw, ok)
	}
	if name, kw, p, tough := e.ViewCharacteristics(bear); name != "Bear" || p != 4 || tough != 4 || !slices.Equal(kw, []string{"Trample"}) {
		t.Fatalf("ViewCharacteristics = %q %v %d/%d", name, kw, p, tough)
	}
	// A layer-3 rename anywhere sends ViewCharacteristics (not
	// Characteristics) to the full derivation.
	e.AddContinuous(ContinuousEffect{Source: elk, Controller: 0, Affects: "Card.Self", Layer: LText, SetName: "Moose"})
	e.active()
	if _, _, _, _, ok := e.printedViewCharacteristics(bear); ok {
		t.Fatal("view fast path taken with a rename in play")
	}
	if name, _, _, _ := e.ViewCharacteristics(elk); name != "Moose" {
		t.Fatalf("renamed elk = %q", name)
	}
	check(bear, true, 4, 4, []string{"Trample"})

	// The elk gains flying through its own Card.Self effect: it leaves the
	// fast path, the bear keeps it.
	e.AddContinuous(ContinuousEffect{Source: elk, Controller: 0, Affects: "Card.Self",
		Layer: LAbilities, AddKeywords: []string{"Flying"}})
	check(elk, false, 3, 3, []string{"Flying"})
	check(bear, true, 4, 4, []string{"Trample"})

	// A keyword lord leaves P/T alone: Characteristics takes the full
	// derivation, the P/T-only fast path still answers.
	onBoard(t, e, 0, "Name:Ascend Lord\nManaCost:2\nTypes:Creature Lord\nPT:1/1\nS:Mode$ Continuous | Affected$ Creature.Other+YouCtrl | AddKeyword$ Vigilance | Description$ x\nOracle:x\n")
	check(bear, false, 4, 4, []string{"Trample", "Vigilance"})
	e.active()
	if p, tough, ok := e.printedPT(e.G.Obj(bear), e.G.Obj(bear).Face(), e.active()); !ok || p != 4 || tough != 4 {
		t.Fatalf("P/T fast path with a keyword lord: %d/%d %v", p, tough, ok)
	}
	if e.Power(bear) != 4 || e.Toughness(elk) != 3 {
		t.Fatalf("Power/Toughness = %d/%d", e.Power(bear), e.Toughness(elk))
	}

	// A P/T lord's static can reach any creature: every object takes the full
	// derivation.
	onBoard(t, e, 0, "Name:Lord\nManaCost:2\nTypes:Creature Lord\nPT:1/1\nS:Mode$ Continuous | Affected$ Creature.Other+YouCtrl | AddPower$ 1 | AddToughness$ 1 | Description$ x\nOracle:x\n")
	check(bear, false, 5, 5, []string{"Trample", "Vigilance"})
	check(elk, false, 4, 4, []string{"Flying", "Vigilance"})
	if _, _, ok := e.printedPT(e.G.Obj(bear), e.G.Obj(bear).Face(), e.active()); ok {
		t.Fatal("P/T fast path taken with a P/T lord in play")
	}
	if printedFast(e, inHand) {
		t.Fatal("hand card took the fast path with a lord in play")
	}
}
