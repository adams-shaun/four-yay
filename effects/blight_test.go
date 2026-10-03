package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The Blight (CR 701.60) primitive's effects-side leaves. The real-corpus
// end-to-end pins live in rules/blight_test.go; these exercise the per-player
// ask/answer machinery against hand-built boards, where the strict-supersets
// gate, the re-entry cursor and the fail-closed guards are each isolated.

// blightBoard builds a 3-seat game with named creatures for the seats the
// test cares about, mirroring fixtureHost's mkCard idiom.
func blightBoard(t *testing.T, seats int, perSeat map[state.PlayerID][]string) (*askHost, *Ctx, map[string]state.ObjID) {
	t.Helper()
	h := &askHost{}
	h.g = state.NewGame(names(seats))
	ids := map[string]state.ObjID{}
	for p, kinds := range perSeat {
		for _, kind := range kinds {
			card := mkCard(t, "Name:Fixture "+kind+"\nTypes:Creature\nPT:1/1\nOracle:x\n")
			o := h.g.AddObject(card, p)
			o.Zone = state.ZBattlefield
			h.g.SetZone(state.ZBattlefield, p, append(h.g.Zone(state.ZBattlefield, p), o.ID))
			ids[kind] = o.ID
		}
	}
	return h, &Ctx{Source: ids["you1"], Controller: 0}, ids
}

// countersOf collects the log's M1M1 CounterChange events as id→amount.
func blightCounters(t *testing.T, h *askHost) map[state.ObjID]int32 {
	t.Helper()
	out := map[state.ObjID]int32{}
	for _, ev := range h.log {
		if ev.Kind != events.CounterChange || ev.Counter != "M1M1" {
			continue
		}
		out[ev.Obj] += ev.Amount
	}
	return out
}

// TestBlightOneCreaturePlacesSilentlyWithoutAsking pins the strict-supersets
// gate's silent leg (the effDiscard TgtChoose rule): with exactly one eligible
// creature the deterministic answer IS the only legal answer, so the counter
// is placed with no decision and no Note.
func TestBlightOneCreaturePlacesSilentlyWithoutAsking(t *testing.T) {
	h, c, ids := blightBoard(t, 2, map[state.PlayerID][]string{0: {"you1"}})
	Resolve(h, c, sa(t, "DB$ Blight | Defined$ You | Num$ 1"))
	if h.asked != nil {
		t.Fatalf("asked %+v, want a silent single-creature placement", h.asked)
	}
	got := blightCounters(t, h)
	if got[ids["you1"]] != 1 {
		t.Fatalf("counters = %v, want 1 on you1 (%d)", got, ids["you1"])
	}
	for _, ev := range h.log {
		if ev.Kind == events.Note {
			t.Fatalf("unexpected Note %q on the no-choice path", ev.Text)
		}
	}
}

// TestBlightOpponentBlightsTheirOwnCreature pins the chooser rule: Defined$
// Opponent (High Perfect Morcant's shape) makes the OPPONENT the blighting
// player — the controller's creatures are never touched, and an opponent with
// no creature is unharmed.
func TestBlightOpponentBlightsTheirOwnCreature(t *testing.T) {
	h, c, ids := blightBoard(t, 3, map[state.PlayerID][]string{
		0: {"you1"},
		1: {"opp1"},
		// seat 2 controls no creature
	})
	Resolve(h, c, sa(t, "DB$ Blight | Defined$ Opponent | Num$ 1"))
	got := blightCounters(t, h)
	if got[ids["opp1"]] != 1 {
		t.Fatalf("counters = %v, want 1 on the opponent's opp1 (%d)", got, ids["opp1"])
	}
	if got[ids["you1"]] != 0 {
		t.Fatalf("the controller's own creature was blighted: %v", got)
	}
}

// TestBlightNoCreatureIsANoOp pins the CR 701.60 no-creature leg: a player
// controlling no creature blights nothing and nothing else happens.
func TestBlightNoCreatureIsANoOp(t *testing.T) {
	h, c, ids := blightBoard(t, 3, map[state.PlayerID][]string{0: {"you1"}})
	Resolve(h, c, sa(t, "DB$ Blight | Defined$ Opponent | Num$ 2"))
	if got := blightCounters(t, h); len(got) != 0 {
		t.Fatalf("counters = %v, want none", got)
	}
	if h.asked != nil {
		t.Fatalf("asked %+v with an empty battlefield", h.asked)
	}
	_ = ids
}

// TestBlightTargetedOpponentIsTheBlighter pins the ValidTgts$ fallback
// (Champion of the Weird's AB shape): with no Defined$ at all the chosen
// target — the targeted opponent, not the ability's controller — blights.
func TestBlightTargetedOpponentIsTheBlighter(t *testing.T) {
	h, _, ids := blightBoard(t, 2, map[state.PlayerID][]string{
		0: {"you1"},
		1: {"opp1"},
	})
	c := &Ctx{Source: ids["you1"], Controller: 0,
		Targets: []state.Target{{Player: 1, IsPlayer: true}}, TargetsOffered: true}
	Resolve(h, c, sa(t, "AB$ Blight | ValidTgts$ Opponent | Num$ 2"))
	got := blightCounters(t, h)
	if got[ids["opp1"]] != 2 {
		t.Fatalf("counters = %v, want 2 on the targeted opponent's opp1", got)
	}
	if got[ids["you1"]] != 0 {
		t.Fatalf("the activator's creature was blighted: %v", got)
	}
}

// TestBlightNumZeroIsANoOp pins the Num clamp: an unresolvable Num$ degrades
// to the documented zero and never poses a 0..0 ask (the r2 dig gate).
func TestBlightNumZeroIsANoOp(t *testing.T) {
	h, c, _ := blightBoard(t, 2, map[state.PlayerID][]string{0: {"you1", "you2"}})
	Resolve(h, c, sa(t, "DB$ Blight | Defined$ You | Num$ 0"))
	if got := blightCounters(t, h); len(got) != 0 {
		t.Fatalf("counters = %v, want none", got)
	}
	if h.asked != nil {
		t.Fatalf("asked %+v for a Num$ 0 blight", h.asked)
	}
}

// TestBlightUnknownParamFailsClosedLoudly pins the out-of-scope pattern: a
// parameter this build does not read emits ONE loud Note naming it and the
// body no-ops, never a silent guess.
func TestBlightUnknownParamFailsClosedLoudly(t *testing.T) {
	h, c, _ := blightBoard(t, 2, map[state.PlayerID][]string{0: {"you1"}})
	Resolve(h, c, sa(t, "DB$ Blight | Defined$ You | Num$ 1 | RememberObjects$ Targeted"))
	got := blightCounters(t, h)
	if len(got) != 0 {
		t.Fatalf("counters = %v, want none on a fail-closed shape", got)
	}
	found := false
	for _, ev := range h.log {
		if ev.Kind == events.Note && ev.Text == "unimplemented Blight shape: RememberObjects" {
			found = true
		}
	}
	if !found {
		t.Fatalf("no loud unimplemented-shape Note recorded; log = %+v", h.log)
	}
}
