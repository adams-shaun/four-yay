package rules

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// selfOnlyBoard is a token board (n vanilla goblins) plus a Vehicle crewed
// into a creature by a registered `Affected$ Card.Self` layer-4 effect --
// the shape of the cardfuzz hang seed 6181111140895991800, where Clown Car's
// crew effects kept the layer-4 table live while a Krenko doubling put 8k-16k
// goblins on the battlefield, and every emitted event (each token's
// creation, each attacker's tap) rebuilt the table by running the full
// layer-4 walk over every battlefield object.
func selfOnlyBoard(t testing.TB, n int) (*Engine, state.ObjID) {
	t.Helper()
	e := layerEngine(t)
	for i := 0; i < n; i++ {
		onBoard(t, e, 0, fmt.Sprintf("Name:Goblin %d\nTypes:Creature Goblin\nPT:1/1\nOracle:x\n", i))
	}
	car := onBoard(t, e, 0, "Name:Car\nTypes:Artifact Vehicle\nPT:4/4\nOracle:x\n")
	e.AddContinuous(ContinuousEffect{Source: car, Controller: 0, Affects: "Card.Self", Layer: LType,
		UntilEOT: true, AddTypes: []string{"Artifact", "Creature"}})
	return e, car
}

// TestLayer4SelfOnlyTableVisitsOnlyItsSources pins the fast path: while every
// live layer-4 effect is a registered Card.Self effect (and no static can
// emit one), the only battlefield objects whose derived types can differ
// from their face are those effects' sources plus the objects whose type
// BASE is already not the printed face (face down, a CopyNonLegendary copy,
// an attached Bestow/Reconfigure card). The table must be built from those
// alone -- the rest of the board is never walked through the layer-4 match
// -- and must equal the full walk's table exactly.
func TestLayer4SelfOnlyTableVisitsOnlyItsSources(t *testing.T) {
	t.Parallel()
	e, car := selfOnlyBoard(t, 200)
	// A face-down goblin: its CR 708.5 base {Creature} differs from the
	// printed "Creature Goblin", so the full walk lists it and the fast path
	// must too, although no layer-4 effect names it.
	fd := onBoard(t, e, 0, "Name:Hidden Goblin\nTypes:Creature Goblin\nPT:1/1\nOracle:x\n")
	e.G.Obj(fd).FaceDown = true

	srcs, ok := e.layer4SelfOnlySources(nil)
	if !ok || !reflect.DeepEqual(srcs, []state.ObjID{car}) {
		t.Fatalf("layer4SelfOnlySources = %v, %v; want [%d], true", srcs, ok, car)
	}
	got := e.buildDerivedTypes(nil)
	want := e.buildDerivedTypesFull(nil)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("self-only table %v != full walk %v", got, want)
	}
	if len(got) != 2 || got[0].ID != car || got[1].ID != fd {
		t.Fatalf("table = %v; want the crewed car and the face-down goblin", got)
	}

	// Any layer-4 effect that is not a plain Card.Self one (a type lord)
	// disables the fast path: it may reach any object on the board.
	lord := onBoard(t, e, 0, "Name:Lord\nTypes:Enchantment\nOracle:x\n")
	e.AddContinuous(ContinuousEffect{Source: lord, Controller: 0, Affects: "Creature.YouCtrl", Layer: LType,
		UntilEOT: true, AddTypes: []string{"Elf"}})
	if _, ok := e.layer4SelfOnlySources(nil); ok {
		t.Fatal("a Creature.YouCtrl layer-4 effect must disable the self-only fast path")
	}
	if got, want := e.buildDerivedTypes(nil), e.buildDerivedTypesFull(nil); !reflect.DeepEqual(got, want) || len(got) < 200 {
		t.Fatalf("full-walk table has %d entries, want every goblin (%d), equal to buildDerivedTypesFull", len(got), len(want))
	}
}

func BenchmarkLayer4TableSelfOnly(b *testing.B) {
	for _, n := range []int{1000, 8000} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			e, car := selfOnlyBoard(b, n)
			benchWithoutVerify(b)
			// Seed the incremental state with a whole-board build, then
			// measure the two build paths side by side. "full" is the
			// pre-incremental reference (the whole-board self-only walk;
			// still O(board) by design, it is the conservative fallback);
			// "incremental" is what the per-event refresh now uses and must
			// not grow with the board.
			e.refreshDerivedTypes()
			buf := e.buildDerivedTypesIncremental()
			full := e.buildDerivedTypes(nil)
			b.Run("full", func(b *testing.B) {
				b.ReportAllocs()
				for range b.N {
					full = e.buildDerivedTypes(full[:0])
				}
			})
			b.Run("incremental", func(b *testing.B) {
				b.ReportAllocs()
				for range b.N {
					buf = e.buildDerivedTypesIncremental()
				}
			})
			// "refresh" is the per-event path emit actually pays: one
			// non-inert event pending since the last build, so
			// refreshDerivedTypes runs the candidate catch-up, the
			// statics-probe catch-up (its own epoch rewound too) and the
			// incremental build. It must be flat in n as well.
			e.emit(events.Event{Kind: events.Tap, Obj: car})
			head := len(e.L.Events)
			b.Run("refresh", func(b *testing.B) {
				b.ReportAllocs()
				before := e.typesIncrBuilds
				for range b.N {
					e.typesEpoch, e.typesProbeEpoch = head-1, head-1
					e.refreshDerivedTypes()
				}
				if got := e.typesIncrBuilds - before; got != b.N {
					b.Fatalf("refresh took the incremental path %d of %d times", got, b.N)
				}
			})
			buf = e.EffectiveTypes()
			full = e.buildDerivedTypes(nil)
			if len(buf) != len(full) || !reflect.DeepEqual(buf, full) {
				b.Fatalf("incremental table %v != full walk %v", buf, full)
			}
		})
	}
}
