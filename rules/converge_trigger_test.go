package rules

// CR 107.4f-family converge, the TRIGGER-relative spelling (task
// tconverge1): "creatures you control get +1/+0 until end of turn for each
// color of mana spent to cast that spell" reads SVar:Y:TriggeredCard$Converge
// -- a trigger reading a DIFFERENT spell's cast. Before the fix that body fell
// off evalRefProperty's property switch to the default (0, false) and degraded
// to the silent zero, the same signature converge1 closed for the plain
// Count$Converge head; and the pay-time capture gate (faceWantsConverge)
// stamped nothing on an ordinary non-converge cast, so even a fixed read site
// would have read 0. The fix adds the Converge property (provenance snapshot
// beside TriggerPaidX) and the reader-out second arm of the capture gate.
//
// Every fixture here is a real Forge script SHAPE (Magmablood Archaic's trigger
// and SVars are its corpus carrier's, minus the converge etbCounter so the
// stamped cast below is provably an ordinary non-converge face) written inline
// per the repo's licensing rule: never a .cards/ .txt.

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

const magmabloodReaderSrc = "Name:Magmablood Archaic\nManaCost:3 R\nTypes:Creature Avatar\nPT:2/2\n" +
	"T:Mode$ SpellCast | ValidCard$ Instant,Sorcery | ValidActivatingPlayer$ You | TriggerZones$ Battlefield | Execute$ TrigPumpAll | TriggerDescription$ Whenever you cast an instant or sorcery spell, creatures you control get +1/+0 until end of turn for each color of mana spent to cast that spell.\n" +
	"SVar:TrigPumpAll:DB$ PumpAll | ValidCards$ Creature.YouCtrl | NumAtt$ +Y\n" +
	"SVar:X:Count$Converge\n" +
	"SVar:Y:TriggeredCard$Converge\n" +
	"Oracle:x\n"

const emberBoltSrc = "Name:Ember Bolt\nManaCost:2 R\nTypes:Instant\n" +
	"A:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 2\n" +
	"Oracle:x\n"

const pumbaSrc = "Name:Grizzly Bears\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"

// seedTossSeat0 advances the seed until the CR 103.1 toss starts seat 0, so
// the caller can address seat 0 (the same convention seatZeroStart and
// converge_test's noted seeds use; the toss draw precedes every shuffle, so
// the winner is a function of the seed alone).
func seedTossSeat0(seed uint64) uint64 {
	for {
		e := New(Config{Seed: seed, Names: []string{"a", "b"}})
		if e.G.Active == 0 {
			return seed
		}
		seed++
	}
}

// submitTargetTo submits the target decision's option naming the given
// player, so a bolt with ValidTgts$ Any never lands on the test's own bear.
func submitTargetTo(t *testing.T, e *Engine, p state.PlayerID) {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected a target decision, got %+v", d)
	}
	for _, o := range d.Options {
		if o.Kind == "player" && o.Player == p {
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{o.Index}}); err != nil {
				t.Fatalf("submit: %v", err)
			}
			return
		}
	}
	t.Fatalf("no player-%d target option in %+v", p, d.Options)
}

// TestMagmabloodPumpsPerColourOfCastSpell pins the end-to-end shape: a
// two-colour instant cast with the reader out pumps seat 0's creatures by
// exactly +2/+0, the same cast paid one colour pumps by exactly +1 -- two
// distinct values on purpose, because the defect is a silent zero and a
// single-value test can pass by coincidence. The generic deduction spends
// pool colours in the fixed MC,W,U,B,R,G order (the search resolveManaWith
// closes with), so {2}{R} from R,R,G,G spends R pip + R,G generic = two
// colours, and from R,R,R spends one.
func TestMagmabloodPumpsPerColourOfCastSpell(t *testing.T) {
	t.Parallel()
	t.Run("two colours of mana", func(t *testing.T) {
		e, cfg, find := etbConfig(t, seedTossSeat0(141), []string{magmabloodReaderSrc, pumbaSrc, emberBoltSrc}, nil)
		reader := find("Magmablood Archaic", 0)
		bearID := find("Grizzly Bears", 0)
		putCreature(t, e, 0, magmabloodReaderSrc)
		putCreature(t, e, 0, pumbaSrc)
		addMana(t, e, 0, "RRGG") // {2}{R} paid R + 2R,1G generic = two colours
		castFirst(t, e, "cast")
		submitTargetTo(t, e, 1) // the bolt hits the opponent's face, not the bear
		passUntilStackEmpty(t, e, 20)
		if o := e.G.Obj(reader); o.Zone != state.ZBattlefield {
			t.Fatalf("reader zone=%s, want battlefield", o.Zone)
		}
		if got := e.Power(bearID); got != 4 {
			t.Fatalf("bear power=%d, want 4 (+2/+0 from two colours spent)", got)
		}
		// The reader-out gate arm stamped the ordinary non-converge cast: the
		// pay-time FlagConverged CastInfo is observable in the event stream.
		sawConvergeCastInfo := false
		for _, ev := range e.L.Events {
			if ev.Kind == events.CastInfo && events.FlagsFrom(ev.Counter)&state.FlagConverged != 0 {
				if ev.Amount != 2 {
					t.Fatalf("converge CastInfo Amount=%d, want 2", ev.Amount)
				}
				sawConvergeCastInfo = true
			}
		}
		if !sawConvergeCastInfo {
			t.Fatal("no FlagConverged CastInfo stamped on the two-colour cast with the reader out")
		}
		replayCheck(t, e, cfg)
	})
	t.Run("one colour of mana", func(t *testing.T) {
		e, cfg, find := etbConfig(t, seedTossSeat0(147), []string{magmabloodReaderSrc, pumbaSrc, emberBoltSrc}, nil)
		bearID := find("Grizzly Bears", 0)
		putCreature(t, e, 0, magmabloodReaderSrc)
		putCreature(t, e, 0, pumbaSrc)
		addMana(t, e, 0, "RRR") // {2}{R} paid entirely in R = one colour
		castFirst(t, e, "cast")
		submitTargetTo(t, e, 1)
		passUntilStackEmpty(t, e, 20)
		if got := e.Power(bearID); got != 3 {
			t.Fatalf("bear power=%d, want 3 (+1/+0 from one colour spent)", got)
		}
		replayCheck(t, e, cfg)
	})
}

// TestMagmabloodNoReaderOutStampsNothing pins the gate's negative arm from
// the reader side: the same two-colour cast with the reader still in hand
// emits no FlagConverged CastInfo (nothing on the battlefield could have read
// the value) and the bear is not pumped -- a cast no trigger reads must not
// change an event, which is what keeps the chain heads put for every game
// that holds no reader out.
func TestMagmabloodNoReaderOutStampsNothing(t *testing.T) {
	t.Parallel()
	e, cfg, find := etbConfig(t, seedTossSeat0(153), []string{magmabloodReaderSrc, pumbaSrc, emberBoltSrc}, nil)
	// seed: toss starts seat 0
	boltID := find("Ember Bolt", 0)
	bearID := find("Grizzly Bears", 0)
	putCreature(t, e, 0, pumbaSrc) // the reader stays in hand
	addMana(t, e, 0, "RRGG")
	castNamed(t, e, "Ember Bolt") // the reader (3 R) is also castable from hand
	submitTargetTo(t, e, 1)
	passUntilStackEmpty(t, e, 20)
	for _, ev := range e.L.Events {
		if ev.Kind == events.CastInfo && ev.Obj == boltID && events.FlagsFrom(ev.Counter)&state.FlagConverged != 0 {
			t.Fatal("FlagConverged CastInfo stamped on a cast with no reader out")
		}
	}
	if got := e.Power(bearID); got != 2 {
		t.Fatalf("bear power=%d, want 2 (no reader out, no pump)", got)
	}
	replayCheck(t, e, cfg)
}
