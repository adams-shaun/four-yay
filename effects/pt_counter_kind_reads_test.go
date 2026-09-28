package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

// These tests pin the effects-side P/T reads to the full P/T counter
// vocabulary (state.CounterPTTotals, CR 122.1a/613.7d): a P2P2, M0M1, M1M1
// or P2P0 counter must move every effects-level net-P/T read exactly as it
// moves the rules layer, not just the +1/+1 pair. The five read sites -- the
// Bolster election, the filter grammar's objectPower/objectToughness fallback
// (and the greatestPower classifier it feeds), the CardPower/CardToughness
// property aggregate, refPower/refToughness' off-battlefield/snapshot arm,
// and the budget picker's chooseCardPower fallback -- each get one test.

type ptCounter struct {
	kind string
	n    int32
}

// ptCreature adds one creature for owner with the given printed face plus
// counters of kinds the +1/+1 / -1/-1 pair does not cover, and returns its
// id. The id (never the *state.Object) is what callers keep: Game.AddObject
// appends to Game.Objs, so a pointer taken before a later AddObject goes
// stale (see move_counter_test.go's preamble).
func ptCreature(t *testing.T, g *state.Game, name, pt string, owner state.PlayerID, counters ...ptCounter) state.ObjID {
	t.Helper()
	return ptCard(t, g, name, "Creature", pt, state.ZBattlefield, owner, counters...)
}

// ptCard adds a card of the given types to ANY zone (its id is returned the
// same staleness-safe way), carrying the given counters.
func ptCard(t *testing.T, g *state.Game, name, types, pt string, zone state.Zone, owner state.PlayerID, counters ...ptCounter) state.ObjID {
	t.Helper()
	o := g.AddObject(mkCard(t, "Name:"+name+"\nTypes:"+types+"\nPT:"+pt+"\nOracle:x\n"), owner)
	o.Zone = zone
	g.SetZone(zone, owner, append(g.Zone(zone, owner), o.ID))
	for _, c := range counters {
		o.AddCounter(c.kind, c.n)
	}
	return o.ID
}

// ptNetPT computes the net power and toughness a site is expected to honour:
// the printed face plus the summed P/T counter deltas. It deliberately does
// NOT call the functions under test, so these preconditions still hold when
// the production reads are reverted to their pre-fix spellings and the
// assertions below are the ones that fail.
func ptNetPower(o *state.Object) int32 {
	dp, _ := o.CounterPTTotals()
	return int32(o.Face().Power()) + dp
}
func ptNetToughness(o *state.Object) int32 {
	_, dt := o.CounterPTTotals()
	return int32(o.Face().Toughness()) + dt
}

// TestBolsterElectionHonoursPTCounterKinds: CR 701.36 puts the counter on the
// creature you control with the least toughness, and least-toughness is a net
// read. Each subtest resolves `DB$ PutCounter | Bolster$ 1 | CounterType$
// P1P1` over a board where the two net toughness reads differ but a
// face-plus-P1P1 read ranks them the other way round. A single minimum
// candidate means no tie decision is posed, so the resolution is
// deterministic; the answer is visible in the placed counter.
func TestBolsterElectionHonoursPTCounterKinds(t *testing.T) {
	t.Run("P2P2 counter makes the pumped creature lose the election", func(t *testing.T) {
		g := state.NewGame(names(2))
		pumped := ptCreature(t, g, "Pumped", "2/2", 0, ptCounter{"P2P2", 1})
		plain := ptCreature(t, g, "Plain", "2/3", 0)

		// Precondition: the counter is present and the two net toughness
		// reads actually differ (4 vs 3). Under a face-plus-P1P1 read they
		// would be 2 vs 3, which elects the WRONG creature.
		if got, want := ptNetToughness(g.Obj(pumped)), int32(4); got != want {
			t.Fatalf("precondition: net toughness of Pumped = %d, want %d", got, want)
		}
		if got, want := ptNetToughness(g.Obj(plain)), int32(3); got != want {
			t.Fatalf("precondition: net toughness of Plain = %d, want %d", got, want)
		}

		h := &fakeHost{g: g}
		Resolve(h, &Ctx{Controller: 0},
			sa(t, "DB$ PutCounter | Bolster$ 1 | CounterType$ P1P1"))

		if got := g.Obj(plain).Counter("P1P1"); got != 1 {
			t.Fatalf("Plain (net toughness %d) should have been elected; its P1P1 count = %d",
				objectToughness(g.Obj(plain)), got)
		}
		if got := g.Obj(pumped).Counter("P1P1"); got != 0 {
			t.Errorf("Pumped (net toughness %d) should NOT have been elected; its P1P1 count = %d",
				objectToughness(g.Obj(pumped)), got)
		}
	})

	// The two remaining subtests share one shape: three -X/-Y counters of a
	// single kind shrink "Shrunk" (printed 4/4) to a net toughness of 1,
	// below "Plain"'s printed 2. A face-plus-P1P1 read -- and even a
	// face-plus-P1P1-minus-M1M1 read, which the election used before the
	// fix -- would rank Shrunk at 4 and elect Plain instead.
	for _, tc := range []struct {
		kind string
		note string
	}{
		{"M0M1", "M0M1 counters shrink the candidate into the election"},
		{"M1M1", "M1M1 counters shrink the candidate into the election"},
	} {
		t.Run(tc.note, func(t *testing.T) {
			g := state.NewGame(names(2))
			shrunk := ptCreature(t, g, "Shrunk", "4/4", 0, ptCounter{tc.kind, 3})
			plain := ptCreature(t, g, "Plain", "2/2", 0)

			// Precondition: Shrunk really carries the shrinking counters on
			// a 4-toughness face, and the net toughness reads actually
			// differ between the two candidates (1 vs 2). Under the old read
			// both candidates sit at their printed faces and the election
			// flips.
			s := g.Obj(shrunk)
			if s.Counter(tc.kind) != 3 || s.Face().Toughness() != 4 {
				t.Fatalf("precondition: Shrunk should be a printed 4/4 with three %s counters (count %d, face %d)",
					tc.kind, s.Counter(tc.kind), s.Face().Toughness())
			}
			_, dt := s.CounterPTTotals()
			if dt != -3 {
				t.Fatalf("precondition: Shrunk's %s deltas sum to %d toughness, want -3", tc.kind, dt)
			}
			if got, want := ptNetToughness(s), int32(1); got != want {
				t.Fatalf("precondition: net toughness of Shrunk = %d, want %d", got, want)
			}
			if got, want := ptNetToughness(g.Obj(plain)), int32(2); got != want {
				t.Fatalf("precondition: net toughness of Plain = %d, want %d", got, want)
			}

			h := &fakeHost{g: g}
			Resolve(h, &Ctx{Controller: 0},
				sa(t, "DB$ PutCounter | Bolster$ 1 | CounterType$ P1P1"))

			if got := g.Obj(shrunk).Counter("P1P1"); got != 1 {
				t.Fatalf("Shrunk (net toughness %d) should have been elected; its P1P1 count = %d",
					objectToughness(g.Obj(shrunk)), got)
			}
			if got := g.Obj(plain).Counter("P1P1"); got != 0 {
				t.Errorf("Plain (net toughness %d) should NOT have been elected; its P1P1 count = %d",
					objectToughness(g.Obj(plain)), got)
			}
		})
	}
}

// TestObjectPTFallbackHonoursPTCounterKinds covers objectPower and
// objectToughness, the filter grammar's net-P/T fallback (numericPred's
// power/toughness operands without a rules-bound SpecContext). A P2P2 counter
// must move both reads; M0M1 must move toughness only; a CHARGE counter is
// not a P/T counter kind (CounterPTDelta reports ok=false) and must be
// invisible to the fallback.
func TestObjectPTFallbackHonoursPTCounterKinds(t *testing.T) {
	g := state.NewGame(names(2))
	pumped := ptCreature(t, g, "Pumped", "2/2", 0, ptCounter{"P2P2", 1})
	shrunk := ptCreature(t, g, "Shrunk", "2/2", 0, ptCounter{"M0M1", 1})
	charged := ptCreature(t, g, "Charged", "2/2", 0, ptCounter{"CHARGE", 2})

	a := g.Obj(pumped)
	if a.Face().Power() != 2 || a.Face().Toughness() != 2 || a.Counter("P2P2") != 1 {
		t.Fatalf("precondition: Pumped should be a printed 2/2 carrying one P2P2 counter (face P/T %d/%d, count %d)",
			a.Face().Power(), a.Face().Toughness(), a.Counter("P2P2"))
	}
	if got, want := ptNetPower(g.Obj(pumped)), int32(4); got != want {
		t.Fatalf("precondition: net power of Pumped = %d, want %d", got, want)
	}
	if got, want := ptNetToughness(g.Obj(shrunk)), int32(1); got != want {
		t.Fatalf("precondition: net toughness of Shrunk = %d, want %d", got, want)
	}

	if got, want := objectPower(g.Obj(pumped)), 4; got != want {
		t.Errorf("objectPower with one P2P2 counter = %d, want %d", got, want)
	}
	if got, want := objectToughness(g.Obj(pumped)), 4; got != want {
		t.Errorf("objectToughness with one P2P2 counter = %d, want %d", got, want)
	}
	if got, want := objectToughness(g.Obj(shrunk)), 1; got != want {
		t.Errorf("objectToughness with one M0M1 counter = %d, want %d", got, want)
	}
	// CHARGE is not a P/T counter kind (CounterPTDelta reports ok=false), so
	// it must be invisible to the fallback read.
	c := g.Obj(charged)
	if c.Counter("CHARGE") != 2 || c.Face().Power() != 2 || c.Face().Toughness() != 2 {
		t.Fatalf("precondition: Charged should be a printed 2/2 carrying two CHARGE counters")
	}
	if got, want := objectPower(g.Obj(charged)), 2; got != want {
		t.Errorf("objectPower must ignore CHARGE counters; got %d, want %d", got, want)
	}
	if got, want := objectToughness(g.Obj(charged)), 2; got != want {
		t.Errorf("objectToughness must ignore CHARGE counters; got %d, want %d", got, want)
	}
}

// TestGreatestPowerClassifierHonoursPTCounterKinds: the greatestPower
// classifier calls objectPower directly, so it must see every P/T counter
// kind. "Pumped" carries a P2P2 counter and so has the greatest power (4 vs
// 3); under a face-plus-P1P1 read it would rank BELOW "Plain" and the
// classifier would flip.
func TestGreatestPowerClassifierHonoursPTCounterKinds(t *testing.T) {
	g := state.NewGame(names(2))
	pumped := ptCreature(t, g, "Pumped", "2/2", 0, ptCounter{"P2P2", 1})
	plain := ptCreature(t, g, "Plain", "3/3", 0)

	if got, want := ptNetPower(g.Obj(pumped)), int32(4); got != want {
		t.Fatalf("precondition: net power of Pumped = %d, want %d", got, want)
	}
	if got, want := ptNetPower(g.Obj(plain)), int32(3); got != want {
		t.Fatalf("precondition: net power of Plain = %d, want %d", got, want)
	}

	if got := MatchesSpec(g, "Creature.greatestPower", pumped, 0); !got {
		t.Errorf("Pumped (net power %d) should match greatestPower", objectPower(g.Obj(pumped)))
	}
	if got := MatchesSpec(g, "Creature.greatestPower", plain, 0); got {
		t.Errorf("Plain (net power %d) should not match greatestPower while Pumped is at %d",
			objectPower(g.Obj(plain)), objectPower(g.Obj(pumped)))
	}
}

// TestRefPTFallbackArmsHonourPTCounterKinds covers refPower/refToughness'
// non-battlefield arms: the snapshot read (a referred-to object read without
// the live layer walk) and the off-battlefield read (an object that left the
// battlefield). PutCounter places counters on objects in any zone (CR 122.1),
// so a graveyard object can carry a real P/T counter here.
func TestRefPTFallbackArmsHonourPTCounterKinds(t *testing.T) {
	g := state.NewGame(names(2))
	h := &fakeHost{g: g}

	// Snapshot arm: a battlefield object, but read with snapshot=true, so
	// neither the in-progress layer bind nor Host.Power is consulted.
	snapshot := ptCreature(t, g, "Snapshotted", "2/2", 0, ptCounter{"P2P2", 1})
	if got, want := ptNetPower(g.Obj(snapshot)), int32(4); got != want {
		t.Fatalf("precondition: net power of Snapshotted = %d, want %d", got, want)
	}
	if got, want := ptNetToughness(g.Obj(snapshot)), int32(4); got != want {
		t.Fatalf("precondition: net toughness of Snapshotted = %d, want %d", got, want)
	}

	// Off-battlefield arm: an object that left the battlefield, still
	// carrying a real P/T counter (PutCounter places counters in any zone,
	// CR 122.1).
	gone := ptCard(t, g, "Gone", "Creature", "2/2", state.ZGraveyard, 0, ptCounter{"P2P2", 1})
	if o := g.Obj(gone); o.Zone != state.ZGraveyard || o.Counter("P2P2") != 1 || o.Face().Power() != 2 {
		t.Fatalf("precondition: Gone should be a graveyard 2/2 carrying one P2P2 counter")
	}
	if got, want := ptNetPower(g.Obj(gone)), int32(4); got != want {
		t.Fatalf("precondition: net power of Gone = %d, want %d", got, want)
	}

	if got, want := refPower(h, g.Obj(snapshot), true), int32(4); got != want {
		t.Errorf("refPower(snapshot) with a P2P2 counter = %d, want %d", got, want)
	}
	if got, want := refToughness(h, g.Obj(snapshot), true), int32(4); got != want {
		t.Errorf("refToughness(snapshot) with a P2P2 counter = %d, want %d", got, want)
	}
	if got, want := refPower(h, g.Obj(gone), false), int32(4); got != want {
		t.Errorf("refPower off-battlefield with a P2P2 counter = %d, want %d", got, want)
	}
	if got, want := refToughness(h, g.Obj(gone), false), int32(4); got != want {
		t.Errorf("refToughness off-battlefield with a P2P2 counter = %d, want %d", got, want)
	}

	// The -X/-Y spelling must be honoured too: three M0M1 counters on a 5/5
	// leave a net toughness of 2, not the printed 5.
	buried := ptCard(t, g, "Buried", "Creature", "5/5", state.ZGraveyard, 0, ptCounter{"M0M1", 3})
	if got, want := refToughness(h, g.Obj(buried), false), int32(2); got != want {
		t.Errorf("refToughness off-battlefield with three M0M1 counters = %d, want %d", got, want)
	}
}

// TestCardPowerPropertyHonoursPTCounterKinds covers the Count$Valid-spec
// "$Property" aggregate (CardPower/CardToughness): the per-object
// objectProperty read and the sum the zone-count fold produces, driven
// end-to-end through EvalCount exactly as Mosswort Bridge's gate reads it.
func TestCardPowerPropertyHonoursPTCounterKinds(t *testing.T) {
	g := state.NewGame(names(2))
	pumped := ptCreature(t, g, "Pumped", "2/2", 0, ptCounter{"P2P2", 1})
	plain := ptCreature(t, g, "Plain", "3/3", 0)

	if got, want := ptNetPower(g.Obj(pumped)), int32(4); got != want {
		t.Fatalf("precondition: net power of Pumped = %d, want %d", got, want)
	}
	if got, want := ptNetPower(g.Obj(plain)), int32(3); got != want {
		t.Fatalf("precondition: net power of Plain = %d, want %d", got, want)
	}

	h := &fakeHost{g: g}
	c := &Ctx{Controller: 0}
	if got, want := EvalCount(h, c, "Count$Valid Creature.YouCtrl$CardPower"), int32(7); got != want {
		t.Errorf("Count$Valid Creature.YouCtrl$CardPower = %d, want %d", got, want)
	}
	if got, want := EvalCount(h, c, "Count$Valid Creature.YouCtrl$CardToughness"), int32(7); got != want {
		t.Errorf("Count$Valid Creature.YouCtrl$CardToughness = %d, want %d", got, want)
	}
	if got, want := EvalCount(h, c, "Count$Valid Creature.YouCtrl"), int32(2); got != want {
		t.Errorf("Count$Valid Creature.YouCtrl = %d, want %d (bare count, no property)", got, want)
	}
}

// TestChooseCardPowerFallbackArmHonoursPTCounterKinds covers the budget
// picker's off-battlefield arm (WithTotalPower$ over a graveyard or library
// pool). PutCounter places counters on objects in any zone (CR 122.1), so a
// graveyard card can carry a real P/T counter here.
func TestChooseCardPowerFallbackArmHonoursPTCounterKinds(t *testing.T) {
	g := state.NewGame(names(2))
	h := &fakeHost{g: g}

	buried := ptCard(t, g, "Buried", "Creature", "2/2", state.ZGraveyard, 0, ptCounter{"P2P2", 1})
	if o := g.Obj(buried); o.Zone != state.ZGraveyard || o.Counter("P2P2") != 1 || o.Face().Power() != 2 {
		t.Fatalf("precondition: Buried should be a graveyard 2/2 carrying one P2P2 counter")
	}
	if got, want := ptNetPower(g.Obj(buried)), int32(4); got != want {
		t.Fatalf("precondition: net power of Buried = %d, want %d", got, want)
	}

	library := ptCard(t, g, "Library", "Creature", "2/2", state.ZLibrary, 0, ptCounter{"P2P0", 1})
	if o := g.Obj(library); o.Zone != state.ZLibrary || o.Counter("P2P0") != 1 {
		t.Fatalf("precondition: Library should be a library 2/2 carrying one P2P0 counter")
	}

	if got, want := chooseCardPower(h, buried), 4; got != want {
		t.Errorf("chooseCardPower with a P2P2 counter = %d, want %d", got, want)
	}
	if got, want := chooseCardPower(h, library), 4; got != want {
		t.Errorf("chooseCardPower with a P2P0 counter = %d, want %d", got, want)
	}
}

// TestPTCounterKindsArePurelyCumulative guards the aggregate contract the
// sites above share: mixed kinds on ONE object sum, and a zero-count entry
// contributes nothing.
func TestPTCounterKindsArePurelyCumulative(t *testing.T) {
	g := state.NewGame(names(2))
	id := ptCreature(t, g, "Mixed", "2/2", 0, ptCounter{"P2P2", 1}, ptCounter{"M0M1", 2}, ptCounter{"CHARGE", 1})

	o := g.Obj(id)
	if got, want := objectPower(o), 4; got != want {
		t.Errorf("objectPower with mixed kinds = %d, want %d (P2P2 applies, M0M1 and CHARGE do not)", got, want)
	}
	if got, want := objectToughness(o), 2; got != want {
		t.Errorf("objectToughness with mixed kinds = %d, want %d (+2 from P2P2, -2 from two M0M1)", got, want)
	}
	// A placed-then-removed counter leaves a zero-count entry; it must
	// contribute nothing.
	o.AddCounter("M1M1", 2)
	o.AddCounter("M1M1", -2)
	if got, want := objectToughness(o), 2; got != want {
		t.Errorf("objectToughness after a placed-then-removed M1M1 pair = %d, want %d", got, want)
	}
	if got, want := objectPower(o), 4; got != want {
		t.Errorf("objectPower after a placed-then-removed M1M1 pair = %d, want %d", got, want)
	}
}
