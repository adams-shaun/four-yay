package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// seqRandHost answers Rand from a fixed script (cycled), counting draws.
type seqRandHost struct {
	*fakeHost
	seq   []int
	draws int
}

func (h *seqRandHost) Rand(n int) int {
	v := h.seq[h.draws%len(h.seq)] % n
	h.draws++
	return v
}

// TestPutCounterSplitRandomDrawsEachCounter pins DividedRandomly$: each
// counter goes to a recipient drawn from the rng, the total is conserved,
// and a single recipient takes the whole total without a draw.
func TestPutCounterSplitRandomDrawsEachCounter(t *testing.T) {
	base := newHost(t, 2)
	h := &seqRandHost{fakeHost: base, seq: []int{1, 1, 0, 1, 2}}
	var ids []state.ObjID
	for range 3 {
		o := base.g.AddObject(mkCard(t, "Name:C\nTypes:Creature\nPT:2/2\nOracle:x\n"), 1)
		base.Emit(events.Event{Kind: events.MoveZone, Obj: o.ID, From: state.ZLibrary, To: state.ZBattlefield})
		ids = append(ids, o.ID)
	}
	placed := putCounterSplitRandom(h, 5, "M0M1", objTargets(ids))
	if h.draws != 5 {
		t.Fatalf("draws = %d, want one per counter (5)", h.draws)
	}
	want := []int32{1, 3, 1} // draws 1,1,0,1,2
	for i, id := range ids {
		if got := base.g.Obj(id).Counter("M0M1"); got != want[i] {
			t.Fatalf("recipient %d has %d counters, want %d", i, got, want[i])
		}
	}
	if len(placed) != 3 {
		t.Fatalf("placed = %v, want all three recipients", placed)
	}
	h.draws = 0
	putCounterSplitRandom(h, 4, "M0M1", objTargets(ids[:1]))
	if h.draws != 0 || base.g.Obj(ids[0]).Counter("M0M1") != 5 {
		t.Fatalf("single recipient: draws %d, counters %d, want 0 draws and 1+4", h.draws, base.g.Obj(ids[0]).Counter("M0M1"))
	}
}
