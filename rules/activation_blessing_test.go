package rules

// `Activation$ Blessing` (task activation-blessing-gate, CR 702.131): the
// "Activate only if you have the city's blessing" activation condition.
// rules/legal.go's activationConditionOK previously failed closed on the
// Blessing token because the latch was untracked; the Ascend machinery
// (rules/ascend.go, state.Player.Blessing, events.BlessingChange) now
// maintains it, so the gate reads the same one-way bit the bare
// Condition$ Blessing gate and the Count$Blessing.<yes>.<no> branch head
// read. The pins below drive the REAL offer path on the real corpus carrier
// (Arch of Orazca) and grant the blessing through the REAL Ascend event path,
// so the gate is proven on the state it gates on, not a hand-set flag alone.

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// archDrawOffered reports whether the priority decision offers Arch of
// Orazca's {5},{T} draw -- the second printed ability (Ability index 1) whose
// Activation$ Blessing gate this task implements. The fixture asserts it is
// at priority and that the FIRST ability (the {T}: Add {C} mana ability) is
// still offered, so a failure to reach the offer loop cannot masquerade as
// the gate withholding the draw.
func archDrawOffered(t *testing.T, e *Engine, arch state.ObjID) bool {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("not at priority: %+v", d)
	}
	sawMana := false
	draw := false
	for _, o := range d.Options {
		if o.Obj != arch {
			continue
		}
		if o.Kind != "activate" && o.Kind != "ability" {
			continue
		}
		if o.Ability == 0 {
			sawMana = true
		}
		if o.Ability == 1 {
			draw = true
		}
	}
	if !sawMana {
		t.Fatalf("the {T}: Add {C} mana ability was not offered, so the draw's absence cannot be read as the gate: %+v", d.Options)
	}
	return draw
}

// fundColorless emits n units of colourless mana into the given seat's pool
// so a {n} activation's cost is payable and the offer loop reaches the
// ability (the Sea Gate Wreckage fixture's ManaAdd shape).
func fundColorless(e *Engine, p state.PlayerID, n int) {
	for i := 0; i < n; i++ {
		e.emit(events.Event{Kind: events.ManaAdd, Player: p, Counter: "C", Amount: 1})
	}
}

// TestArchOfOrazcaBlessingGateIsExact pins the done-when: the land's draw
// ability is offered exactly when the seat holds the city's blessing, and
// never before. The blessing is granted through the REAL Ascend path (ten
// permanents under the Arch's own K:Ascend), the tenth +1/-1 seat-shape the
// latch's other pins use.
func TestArchOfOrazcaBlessingGateIsExact(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	// Nine filler Bears eventlessly, then the Arch enters through the REAL
	// MoveZone path so Engine.emit's Ascend scan runs: nine + the Arch is
	// ten permanents, so the Arch's own arrival grants the blessing.
	vanillaBears(t, e, 0, 9)
	arch := enterOnBattlefield(t, e, 0, corpusCard(t, "Arch of Orazca"))
	// The fixture's precondition: the blessing really was latched by the
	// arrival, not assumed. Without the latch the draw assertion below
	// would be asserting on the wrong state.
	if !e.G.Players[0].Blessing {
		t.Fatalf("ten permanents with the Arch's K:Ascend did not grant the blessing; the gate cannot be tested")
	}
	e.G.Step = state.StepMain1
	e.G.Active, e.G.Priority = 0, 0
	e.G.Turn = 1
	// Fund the {5} activation so the offer loop reaches the ability; the
	// gate is what withholds it, not an unpayable cost.
	fundColorless(e, 0, 5)
	e.priorityRound()
	if !archDrawOffered(t, e, arch) {
		t.Fatal("the blessed draw was not offered")
	}

	// Below ten permanents (the Arch withdrawn so it does not count itself)
	// the latch is never held fresh, so the gate must withhold the draw. The
	// blessing is one-way in play, but this fixture never granted it: the
	// board is rebuilt at nine permanents from a clean engine.
	e2 := layerEngine(t)
	vanillaBears(t, e2, 0, 8)
	arch2 := enterOnBattlefield(t, e2, 0, corpusCard(t, "Arch of Orazca"))
	if e2.G.Players[0].Blessing {
		t.Fatalf("nine permanents granted the blessing; the negative leg is vacuous")
	}
	e2.G.Step = state.StepMain1
	e2.G.Active, e2.G.Priority = 0, 0
	e2.G.Turn = 1
	fundColorless(e2, 0, 5)
	e2.priorityRound()
	if archDrawOffered(t, e2, arch2) {
		t.Fatal("the draw was offered without the city's blessing")
	}
}
