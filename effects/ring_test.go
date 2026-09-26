package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// ringCreature places a fixture creature directly in p's battlefield zone,
// in zone order (the ordered list the bearer walk reads).
func ringCreature(t *testing.T, h *fakeHost, p state.PlayerID, name string) state.ObjID {
	t.Helper()
	card := mkCard(t, "Name:"+name+"\nTypes:Creature\nPT:2/2\nOracle:x\n")
	o := h.g.AddObject(card, p)
	o.Zone = state.ZBattlefield
	h.g.SetZone(state.ZBattlefield, p, append(h.g.Zone(state.ZBattlefield, p), o.ID))
	return o.ID
}

// TestRingTemptsYouCountsAndDesignatesFirstCreature is the no-host fallback
// leaf: with no engine host to ask (this effects double's Ask reports false)
// two eligible creatures pose the R-9 stand-in, and one Resolve emits exactly
// one RingTemptsYou event, the seat's tempt count rises to 1, and the first
// creature in the controller's battlefield zone order is designated. The
// hosted ask path (a real KChoose posed when two or more creatures are
// eligible) is pinned in ring_bearer_choice_test.go.
func TestRingTemptsYouCountsAndDesignatesFirstCreature(t *testing.T) {
	h := newHost(t, 2)
	bear := ringCreature(t, h, 0, "Bear")
	gorilla := ringCreature(t, h, 0, "Gorilla")
	Resolve(h, &Ctx{Controller: 0}, sa(t, "DB$ RingTemptsYou"))
	// Precondition: the ask path was actually reached (two eligible creatures),
	// so the R-9 Note below is the fallback, not the single-creature silent path.
	if h.askCount != 1 {
		t.Fatalf("asks = %d, want 1 (two eligible creatures pose the choice)", h.askCount)
	}
	if h.askResult {
		t.Fatal("precondition: the double must report no host (Ask false)")
	}
	if !ringLogHasNoHostNote(h.log) {
		t.Fatalf("log = %+v, want the R-9 no-host Note", h.log)
	}
	if len(h.log) != 2 || h.log[1].Kind != events.RingTemptsYou || h.log[1].Player != 0 {
		t.Fatalf("log = %+v", h.log)
	}
	if h.log[1].Obj != bear || h.log[1].Amount != 1 {
		t.Fatalf("bearer = %d (want first-in-zone-order %d), amount = %d (want 1)",
			h.log[1].Obj, bear, h.log[1].Amount)
	}
	if h.g.Players[0].RingTempted != 1 || h.g.Players[0].RingBearer != bear {
		t.Fatalf("fold: tempted %d bearer %d, want 1/%d",
			h.g.Players[0].RingTempted, h.g.Players[0].RingBearer, bear)
	}
	_ = gorilla
}

// ringLogHasNoHostNote reports whether the log carries the R-9 no-host Note
// effRingTemptsYou emits when it falls back to the default bearer.
func ringLogHasNoHostNote(log []events.Event) bool {
	for _, e := range log {
		if e.Kind == events.Note && e.Text == "designates the default Ring-bearer (no engine host to ask)" {
			return true
		}
	}
	return false
}

// TestRingTemptsYouKeepsTheExistingBearer is the brief's second leaf: a
// second temptation increments the count to 2 and does NOT re-designate
// away from the existing Ring-bearer — CR 701.54a: the creature "becomes
// your Ring-bearer until another creature becomes your Ring-bearer", so a
// still-controlled bearer keeps the designation even when it is NOT first in
// zone order (here Bear is first, Gorilla is the bearer).
func TestRingTemptsYouKeepsTheExistingBearer(t *testing.T) {
	h := newHost(t, 2)
	ringCreature(t, h, 0, "Bear")
	gorilla := ringCreature(t, h, 0, "Gorilla")
	// A first temptation (a real, Apply-folded event) designates the
	// second-in-order Gorilla — the shape where keep-existing and
	// first-in-order can disagree.
	h.Emit(events.Event{Kind: events.RingTemptsYou, Player: 0, Obj: gorilla, Amount: 1})
	Resolve(h, &Ctx{Controller: 0}, sa(t, "DB$ RingTemptsYou"))
	var tempts int
	for _, e := range h.log {
		if e.Kind == events.RingTemptsYou {
			tempts++
		}
	}
	if tempts != 2 {
		t.Fatalf("tempt events = %d, want 2", tempts)
	}
	if h.g.Players[0].RingTempted != 2 || h.g.Players[0].RingBearer != gorilla {
		t.Fatalf("count %d bearer %d, want 2/%d — the bearer was re-designated away",
			h.g.Players[0].RingTempted, h.g.Players[0].RingBearer, gorilla)
	}
}

// TestRingTemptsYouWithNoCreatureStillCounts is CR 701.54d: the temptation
// completes "even if some [of the actions] were impossible" — a player who
// controls no creature emits the event with Obj 0 and still counts.
func TestRingTemptsYouWithNoCreatureStillCounts(t *testing.T) {
	h := newHost(t, 2)
	Resolve(h, &Ctx{Controller: 0}, sa(t, "DB$ RingTemptsYou"))
	if len(h.log) != 1 || h.log[0].Kind != events.RingTemptsYou || h.log[0].Obj != 0 {
		t.Fatalf("log = %+v, want one Obj-0 temptation", h.log)
	}
	if h.g.Players[0].RingTempted != 1 || h.g.Players[0].RingBearer != 0 {
		t.Fatalf("count %d bearer %d, want 1/0", h.g.Players[0].RingTempted, h.g.Players[0].RingBearer)
	}
}

// TestRingTemptsYouReplacesAStaleBearer: the keep-existing read is a LIVE
// battlefield read — a bearer that left the battlefield is gone (Apply's
// leave-clear wiped the designation), so the next temptation designates the
// next creature in zone order.
func TestRingTemptsYouReplacesAStaleBearer(t *testing.T) {
	h := newHost(t, 2)
	bear := ringCreature(t, h, 0, "Bear")
	gorilla := ringCreature(t, h, 0, "Gorilla")
	h.Emit(events.Event{Kind: events.RingTemptsYou, Player: 0, Obj: bear, Amount: 1})
	h.Emit(events.Event{Kind: events.MoveZone, Obj: bear, From: state.ZBattlefield, To: state.ZGraveyard})
	if h.g.Players[0].RingBearer != 0 {
		t.Fatalf("bearer %d survived leaving the battlefield", h.g.Players[0].RingBearer)
	}
	Resolve(h, &Ctx{Controller: 0}, sa(t, "DB$ RingTemptsYou"))
	if h.g.Players[0].RingTempted != 2 || h.g.Players[0].RingBearer != gorilla {
		t.Fatalf("count %d bearer %d, want 2/%d",
			h.g.Players[0].RingTempted, h.g.Players[0].RingBearer, gorilla)
	}
}

// TestIsRingbearerPredicateMatchesTheDesignatedBearer: the filter predicate
// is recognised by the matcher AND by UnknownPredicates (the shared
// wordPredicate classifier), matches the designated bearer on the
// battlefield, and fails for a non-bearer.
func TestIsRingbearerPredicateMatchesTheDesignatedBearer(t *testing.T) {
	h := newHost(t, 2)
	bear := ringCreature(t, h, 0, "Bear")
	gorilla := ringCreature(t, h, 0, "Gorilla")
	h.Emit(events.Event{Kind: events.RingTemptsYou, Player: 0, Obj: bear, Amount: 1})
	if u := UnknownPredicates("Creature.YouCtrl+IsRingbearer"); len(u) != 0 {
		t.Fatalf("UnknownPredicates = %v, want none", u)
	}
	for _, tc := range []struct {
		id   state.ObjID
		want bool
		why  string
	}{
		{bear, true, "the designated bearer"},
		{gorilla, false, "a non-bearer"},
	} {
		if got := MatchesSpecCtx(h.g, "Creature.IsRingbearer", tc.id, SpecContext{You: 0}); got != tc.want {
			t.Fatalf("%s: IsRingbearer = %v, want %v", tc.why, got, tc.want)
		}
	}
}
