package rules

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// An eventless object insertion must not masquerade as an ordinary emitted
// change; the length guard forces a full refresh and reseeds the index.
func TestLayer4EventlessObjectCountForcesFullRefresh(t *testing.T) {
	t.Parallel()
	e, car, tap := incrBoard(t, 500)
	incrBoardOK(t, e, car) // proves the self-only type grant changes the table
	beforeEpoch, beforeObjs := e.typesEpoch, len(e.G.Objs)
	// Do not use onBoard: that helper deliberately invalidates the key and
	// refreshes immediately. Here the raw, eventless append is the stimulus.
	o := e.G.AddObject(card(t, "Name:Extra Goblin\nTypes:Creature Goblin\nPT:1/1\nOracle:x\n"), 0)
	o.Zone = state.ZBattlefield
	e.G.SetZone(state.ZBattlefield, 0, append(e.G.Zone(state.ZBattlefield, 0), o.ID))
	if o.Zone != state.ZBattlefield || len(e.G.Objs) != beforeObjs+1 || len(e.L.Events) != beforeEpoch || e.typesEpoch != beforeEpoch {
		t.Fatalf("eventless AddObject precondition: added=%d objs=%d (was %d) events=%d (was %d) typesEpoch=%d", o.ID, len(e.G.Objs), beforeObjs, len(e.L.Events), beforeEpoch, e.typesEpoch)
	}
	e.refreshDerivedTypes()
	if e.typesVisited != len(e.G.Objs) {
		t.Fatalf("eventless object-count change examined %d objects, want full board of %d", e.typesVisited, len(e.G.Objs))
	}
	if got, want := e.EffectiveTypes(), e.buildDerivedTypesFull(nil); !reflect.DeepEqual(got, want) || len(got) != 1 || got[0].ID != car {
		t.Fatalf("eventless refresh table %v, want full table %v with car %d", got, want, car)
	}
	before := e.typesIncrBuilds
	emitRefresh(e, tap, 1)
	if e.typesIncrBuilds != before+1 || e.typesVisited >= len(e.G.Objs) {
		t.Fatalf("after reseeding, emitted event did not use incremental refresh (builds %d -> %d, visited %d of %d)", before, e.typesIncrBuilds, e.typesVisited, len(e.G.Objs))
	}
}

// Isolate the old dominant term: a precheck with exactly one logged event
// pending, before and after growing the board eightfold. The cached probe
// must revisit only that event's referent, not the rest of the battlefield.
func BenchmarkLayer4StaticProbeCatchUp(b *testing.B) {
	for _, n := range []int{1000, 8000} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			e, car, tap := incrBoard(b, n)
			incrBoardOK(b, e, car)
			e.emit(events.Event{Kind: events.Tap, Obj: tap})
			benchWithoutVerify(b)
			b.ResetTimer()
			for range b.N {
				e.typesProbeEpoch = len(e.L.Events) - 1
				if e.staticsMayChangeTypes() {
					b.Fatal("fixture unexpectedly has a type-changing static")
				}
			}
		})
	}
}

// TestLayer4CloneCarriesIncrementalState: a clone of a board whose layer-4
// table is current goes incremental on its first emitted event -- it carries
// the candidate slice, source stamp and statics probe in its own storage --
// and its table matches the whole-board rebuild; the parent is untouched.
func TestLayer4CloneCarriesIncrementalState(t *testing.T) {
	t.Parallel()
	e, car, tap := incrBoard(t, 200)
	incrBoardOK(t, e, car)
	c := e.Clone()
	if !c.typesIncrReady || !c.typesProbeReady || c.typesVersion != c.continuousVersion || c.typesProbeVersion != c.continuousVersion {
		t.Fatalf("clone did not carry the current layer-4 state (incr %v probe %v)", c.typesIncrReady, c.typesProbeReady)
	}
	if len(c.typesProbe) > 0 && &c.typesProbe[0] == &e.typesProbe[0] {
		t.Fatal("clone shares the parent's probe cells")
	}
	before := c.typesIncrBuilds
	emitRefresh(c, tap, 1)
	if c.typesIncrBuilds != before+1 || c.typesVisited >= len(c.G.Objs) {
		t.Fatalf("clone's first refresh was not incremental (builds %d -> %d, visited %d of %d)", before, c.typesIncrBuilds, c.typesVisited, len(c.G.Objs))
	}
	if got, want := c.EffectiveTypes(), c.buildDerivedTypesFull(nil); !reflect.DeepEqual(got, want) {
		t.Fatalf("clone table %v, want full table %v", got, want)
	}
	if e.G.Obj(tap).Counter("P1P1") != 0 {
		t.Fatal("the clone's event reached the parent")
	}
}
