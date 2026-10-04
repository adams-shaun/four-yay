package rules

// Restores effects/move_counter_test.go on the kernel: CounterType$ Any asks
// which kind to move when the origin holds several and the answered kind is
// what moves; CounterNum$ Any asks how many (Min 0 / Max the origin's
// count) and the answered amount -- zero included -- is exactly what moves;
// and Black Panther's RememberAmount$ move feeds its Count$RememberedNumber
// life gain. (The sub-target-beats-parent leaf is
// TestNestingGroundsSubTargetReceivesTheMove.)

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// kr2MoverBoard seats an artifact whose free ability is the MoveCounter body
// (Source$ Self) with the given counters, and a creature to receive them.
func kr2MoverBoard(t *testing.T, body string, counters map[string]int32) (*Engine, state.ObjID, state.ObjID) {
	t.Helper()
	e := kr2Engine(t, 2)
	src := kr2Put(t, e, 0, kr2Src(t, "Name:Mover\nManaCost:1\nTypes:Artifact\nA:AB$ MoveCounter | Cost$ 0 | Source$ Self | ValidTgts$ Creature | "+body+"\nOracle:x\n"), state.ZBattlefield, false)
	dst := kr2Put(t, e, 0, kr2Src(t, "Name:Receiver\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"), state.ZBattlefield, false)
	for _, k := range []string{"P1P1", "CHARGE"} {
		if n := counters[k]; n != 0 {
			e.G.Obj(src).AddCounter(k, n)
		}
	}
	return e, src, dst
}

// kr2MoverAsk activates the mover at dst and returns the first ask after
// the target.
func kr2MoverAsk(t *testing.T, e *Engine, src, dst state.ObjID) *decision.Decision {
	t.Helper()
	d := kr2Activate(t, e, 0, src)
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("activation = %+v, want the creature target ask", d)
	}
	return kr2Answer(t, e, d, kr2ObjIdx(t, d, dst))
}

func TestMoveCounterTypeAnyAsksForTheKind(t *testing.T) {
	t.Parallel()
	e, src, dst := kr2MoverBoard(t, "CounterType$ Any | CounterNum$ 1", map[string]int32{"P1P1": 2, "CHARGE": 3})
	d := kr2Want(t, kr2MoverAsk(t, e, src, dst), "move_counter_kind")
	if d.Kind != decision.KChoose || d.Min != 1 || d.Max != 1 || len(d.Options) != 2 {
		t.Fatalf("kind ask = %+v, want a 1-of KChoose over the two distinct kinds", d)
	}
	if got := e.G.Obj(src).Counter("P1P1"); got != 2 {
		t.Fatalf("source P1P1 = %d before the answer, want 2", got)
	}
	charge := -1
	for _, o := range d.Options {
		if o.Kind == "CHARGE" || o.Label == "CHARGE" || o.Label == "charge" || o.Label == "Charge" {
			charge = o.Index
		}
	}
	if charge < 0 {
		charge = d.Options[1].Index
	}
	if d = kr2Answer(t, e, d, charge); d != nil {
		t.Fatalf("unexpected ask after the kind: %+v", d)
	}
	if got := e.G.Obj(src).Counter("CHARGE"); got != 2 {
		t.Fatalf("source CHARGE = %d, want 2 (one CHARGE moved); options were by label", got)
	}
	if got := e.G.Obj(src).Counter("P1P1"); got != 2 {
		t.Fatalf("source P1P1 = %d, want 2 (a different kind was chosen)", got)
	}
	if got := e.G.Obj(dst).Counter("CHARGE"); got != 1 {
		t.Fatalf("target CHARGE = %d, want 1", got)
	}
}

func TestMoveCounterNumAnyAsksAndHonoursTheAmount(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		n    int32
	}{{"two", 2}, {"decline", 0}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			e, src, dst := kr2MoverBoard(t, "CounterType$ P1P1 | CounterNum$ Any", map[string]int32{"P1P1": 4})
			d := kr2Want(t, kr2MoverAsk(t, e, src, dst), "move_counter")
			if d.Kind != decision.KChoose || d.Min != 0 || d.Max != 4 && len(d.Options) != 5 {
				t.Fatalf("amount ask = %+v, want Min 0 over 0..4", d)
			}
			if len(d.Options) != 5 {
				t.Fatalf("amount options = %d, want 5 (0..4)", len(d.Options))
			}
			if d = kr2Answer(t, e, d, int(tc.n)); d != nil {
				t.Fatalf("unexpected ask after the amount: %+v", d)
			}
			if got := e.G.Obj(src).Counter("P1P1"); got != 4-tc.n {
				t.Fatalf("source P1P1 = %d, want %d", got, 4-tc.n)
			}
			if got := e.G.Obj(dst).Counter("P1P1"); got != tc.n {
				t.Fatalf("target P1P1 = %d, want %d", got, tc.n)
			}
		})
	}
}

func TestBlackPantherRememberAmountFeedsGainLife(t *testing.T) {
	t.Parallel()
	e := kr2Engine(t, 2)
	panther := kr2Put(t, e, 0, kr2Corpus(t, "Black Panther, Wakandan King"), state.ZBattlefield, false)
	land := kr2Put(t, e, 0, kr2Src(t, "Name:Vibranium Land\nTypes:Land\nOracle:x\n"), state.ZBattlefield, false)
	creature := kr2Put(t, e, 0, kr2Src(t, "Name:Target Creature\nManaCost:G\nTypes:Creature\nPT:1/1\nOracle:x\n"), state.ZBattlefield, false)
	e.G.Obj(land).AddCounter("P1P1", 2)
	life := e.G.Players[0].Life
	hand := len(e.G.Zone(state.ZHand, 0))
	d := kr2Activate(t, e, 0, panther)
	for i := 0; d != nil && i < 6; i++ {
		if d.Kind != decision.KTarget {
			t.Fatalf("unexpected decision %+v", d)
		}
		want := creature
		for _, o := range d.Options {
			if o.Obj == land {
				want = land
			}
		}
		d = kr2Answer(t, e, d, kr2ObjIdx(t, d, want))
	}
	if got := e.G.Obj(land).Counter("P1P1"); got != 0 {
		t.Fatalf("land P1P1 = %d, want 0 after moving both", got)
	}
	if got := e.G.Obj(creature).Counter("P1P1"); got != 2 {
		t.Fatalf("creature P1P1 = %d, want 2", got)
	}
	if got := e.G.Players[0].Life; got != life+2 {
		t.Fatalf("life = %d, want %d (X = the two counters RememberAmount$ recorded)", got, life+2)
	}
	if got := len(e.G.Zone(state.ZHand, 0)); got != hand+1 {
		t.Fatalf("hand = %d, want %d (the gated draw ran)", got, hand+1)
	}
	if hasNote(e, "unimplemented API MoveCounter") {
		t.Fatal("compiled MoveCounter did not run")
	}
}
