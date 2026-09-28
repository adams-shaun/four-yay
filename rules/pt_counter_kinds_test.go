package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestWallOfRootsM0M1CountersLowerToughness is the brief's instance test:
// Wall of Roots (0/5) puts a -0/-1 counter on itself as the cost of its mana
// ability (CR 122.1a: a -0/-1 counter gives -0/-1; CR 613.4c layer 7c),
// so after one activation it is 0/4 and after a second activation on a later
// turn it is 0/3. Before the P/T-counter-kind fix the layer walk read only
// P1P1 and M1M1, so both activations left it 0/5.
func TestWallOfRootsM0M1CountersLowerToughness(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	wallCard, ok := reg.Lookup("Wall of Roots")
	if !ok {
		t.Fatal("corpus fixture: Wall of Roots missing")
	}
	e := layerEngine(t)
	wall := onBoardCard(t, e, 0, wallCard)

	// Precondition: the real card is on the battlefield with its printed 0/5
	// and no counters, so the assertions below measure the counter delta and
	// not the printed value.
	o := e.G.Obj(wall)
	if o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Wall of Roots zone = %v, want battlefield", o)
	}
	if o.Counter("M0M1") != 0 {
		t.Fatalf("precondition: Wall of Roots starts with %d -0/-1 counters, want 0", o.Counter("M0M1"))
	}
	if d := e.Derived(wall); d.Power != 0 || d.Toughness != 5 {
		t.Fatalf("precondition: Wall of Roots P/T = %d/%d, want printed 0/5", d.Power, d.Toughness)
	}

	// A logged TurnChange for seat 0 gives the wall a legitimate "under
	// control since this turn began" (CR 302.6) so its {G} mana ability is
	// offered, and it starts the turn whose activation-limit budget the first
	// activation spends.
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: e.G.Turn + 1})
	activateRiderMana(t, e, wall, 0)

	if got := e.G.Obj(wall).Counter("M0M1"); got != 1 {
		t.Fatalf("after one activation: Wall of Roots -0/-1 counters = %d, want 1 (the ability's AddCounter cost)", got)
	}
	if d := e.Derived(wall); d.Power != 0 || d.Toughness != 4 {
		t.Fatalf("after one activation: Wall of Roots P/T = %d/%d, want 0/4 (0/5 plus one -0/-1 counter)", d.Power, d.Toughness)
	}

	// Second activation on a LATER turn (the ability's ActivationLimit$ 1 is
	// per turn). A fresh TurnChange for seat 0 resets the per-turn budget.
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: e.G.Turn + 1})
	activateRiderMana(t, e, wall, 0)

	if got := e.G.Obj(wall).Counter("M0M1"); got != 2 {
		t.Fatalf("after two activations: Wall of Roots -0/-1 counters = %d, want 2", got)
	}
	if d := e.Derived(wall); d.Power != 0 || d.Toughness != 3 {
		t.Fatalf("after two activations: Wall of Roots P/T = %d/%d, want 0/3 (0/5 plus two -0/-1 counters)", d.Power, d.Toughness)
	}
}

// TestPTCounterKindsRaiseAndLowerDerivedPT is the brief's class census as a
// running test: every P/T counter kind the corpus scripts is stamped on a
// vanilla 2/2 through a real CounterChange, and the layer-7d derived P/T must
// honour each one. The kind->delta table is the measured corpus census
// (grep -rhoE 'P[0-9]P[0-9]|M[0-9]M[0-9]' .cards/cardsfolder | sort -u),
// plus the mixed P1M1 spelling the parser also understands.
func TestPTCounterKindsRaiseAndLowerDerivedPT(t *testing.T) {
	t.Parallel()
	cases := []struct {
		kind   string
		dP, dT int32
	}{
		{"P1P1", 1, 1},
		{"P2P2", 2, 2},
		{"P3P3", 3, 3},
		{"P1P0", 1, 0},
		{"P2P0", 2, 0},
		{"P0P1", 0, 1},
		{"P0P2", 0, 2},
		{"P1P2", 1, 2},
		{"M1M1", -1, -1},
		{"M2M2", -2, -2},
		{"M0M1", 0, -1},
		{"M0M2", 0, -2},
		{"M1M0", -1, 0},
		{"M2M0", -2, 0},
		{"M2M1", -2, -1},
		{"P1M1", 1, -1},
	}
	for _, tc := range cases {
		t.Run(tc.kind, func(t *testing.T) {
			t.Parallel()
			e := layerEngine(t)
			bear := onBoard(t, e, 0, "Name:Vanilla 2/2\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
			// Precondition: printed 2/2 with no counters, so the measured P/T
			// below is the printed value plus exactly this counter's delta.
			if d := e.Derived(bear); d.Power != 2 || d.Toughness != 2 {
				t.Fatalf("precondition: vanilla P/T = %d/%d, want printed 2/2", d.Power, d.Toughness)
			}
			e.emit(events.Event{Kind: events.CounterChange, Obj: bear, Counter: tc.kind, Amount: 1})
			if got := e.G.Obj(bear).Counter(tc.kind); got != 1 {
				t.Fatalf("precondition: %s counter on the bear = %d, want 1", tc.kind, got)
			}
			d := e.Derived(bear)
			if d.Power != 2+tc.dP || d.Toughness != 2+tc.dT {
				t.Fatalf("%s counter: derived P/T = %d/%d, want %d/%d", tc.kind, d.Power, d.Toughness, 2+tc.dP, 2+tc.dT)
			}
		})
	}
}
