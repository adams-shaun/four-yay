package botpolicy

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

func simAttack(b Board, ids ...int) (def, sim []int) {
	d := decision.Decision{Seq: 1, Player: 0, Kind: decision.KAttackers, Min: 0, Max: len(ids)}
	for _, id := range ids {
		d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "attacker",
			Obj: state.ObjID(100 + id), Player: 1})
	}
	return Decide(b, &d, rng(1)).Choices, AttackSimDecide(b, &d, rng(1), DefaultAttackSimParams()).Choices
}

// TestAttackSimSwingsPastALoneBlocker is the case the per-attacker veto
// (AR3) cannot see: three 2/2s against one 3/3 and a defender at 5. Each
// 2/2 alone "dies for free" to the 3/3, so the default attacker stays home
// (AR9's swarm test needs 5 guaranteed damage and three attackers past one
// blocker deal only 4). The 3/3 can block only one of them: attacking with
// all three takes the defender to 1 for one 2/2, and the crack-back (the
// 3/3 into two tapped 2/2s) costs 3 of our 20 -- the simulation scores that
// above standing still.
func TestAttackSimSwingsPastALoneBlocker(t *testing.T) {
	b := boardOf(atk(1, 2, 2), atk(2, 2, 2), atk(3, 2, 2), def(1, 3, 3))
	b.Life.Set(0, 20)
	b.Life.Set(1, 5)
	d, s := simAttack(b, 1, 2, 3)
	if len(d) != 0 {
		t.Fatalf("default attacker = %v, want none (the premise)", d)
	}
	if want := []int{0, 1, 2}; !reflect.DeepEqual(s, want) {
		t.Fatalf("attack-sim = %v, want %v", s, want)
	}
}

// TestAttackSimKeepsDefaultWhenIndifferent: an empty defending board and
// no crack-back -- the default answer (swing) is also the simulation's
// best, and the incumbent is returned unchanged.
func TestAttackSimKeepsDefaultWhenIndifferent(t *testing.T) {
	b := boardOf(atk(1, 3, 3))
	b.Life.Set(0, 20)
	b.Life.Set(1, 20)
	d, s := simAttack(b, 1)
	if !reflect.DeepEqual(d, s) || len(s) != 1 {
		t.Fatalf("default %v, attack-sim %v: want both [0]", d, s)
	}
}

// TestAttackSimHoldsBackAgainstLethalCrackBack: at 3 life facing a tapped
// 3/3, our only 2/2's swing is free damage (nothing can block it, AR2), and
// AR4 does not hold it back because the tapped 3/3 cannot block it -- so
// the default attacker swings. But the 3/3 untaps and kills us next turn
// unless the 2/2 stays home to chump; the simulated crack-back sees it.
func TestAttackSimHoldsBackAgainstLethalCrackBack(t *testing.T) {
	b := boardOf(atk(1, 2, 2), def(1, 3, 3))
	c := b.Creatures.Get(201)
	c.Tapped = true
	b.Creatures.Set(201, c)
	b.Life.Set(0, 3)
	b.Life.Set(1, 20)
	d, s := simAttack(b, 1)
	if len(d) != 1 {
		t.Fatalf("default attacker = %v, want the swing (the premise)", d)
	}
	if len(s) != 0 {
		t.Fatalf("attack-sim = %v, want the 2/2 held home against a lethal crack-back", s)
	}
}

// TestAttackSimIsDeterministic: the answer is a pure function of the
// Board and Decision -- Go randomises map iteration, so repeated calls
// over the same maps exercise every order the Board's maps can present.
func TestAttackSimIsDeterministic(t *testing.T) {
	b := boardOf(atk(1, 2, 2), atk(2, 3, 1), atk(3, 1, 4, "Flying"), atk(4, 4, 4, "Trample"),
		def(1, 3, 3), def(2, 2, 2, "Deathtouch"), def(3, 1, 1, "Reach"))
	b.Life.Set(0, 11)
	b.Life.Set(1, 9)
	_, first := simAttack(b, 1, 2, 3, 4)
	for i := 0; i < 50; i++ {
		if _, s := simAttack(b, 1, 2, 3, 4); !reflect.DeepEqual(s, first) {
			t.Fatalf("call %d = %v, first = %v", i, s, first)
		}
	}
	blk := decision.Decision{Seq: 1, Player: 1, Kind: decision.KBlockers, Max: 12}
	for _, bl := range []state.ObjID{201, 202, 203} {
		for _, a := range []state.ObjID{101, 102, 104} {
			blk.Options = append(blk.Options, decision.Option{Index: len(blk.Options), Kind: "block", Obj: bl, Attacker: a, Player: 1})
		}
	}
	for _, a := range []state.ObjID{101, 102, 104} {
		c := b.Creatures.Get(a)
		c.Tapped = true
		b.Creatures.Set(a, c)
	}
	bFirst := AttackSimDecide(b, &blk, rng(1), DefaultAttackSimParams()).Choices
	for i := 0; i < 50; i++ {
		if s := AttackSimDecide(b, &blk, rng(1), DefaultAttackSimParams()).Choices; !reflect.DeepEqual(s, bFirst) {
			t.Fatalf("block call %d = %v, first = %v", i, s, bFirst)
		}
	}
}

// TestAttackSimFallsBackOnSeveralDefenders: the search is two-player; a
// decision naming two defending seats is the default attacker's.
func TestAttackSimFallsBackOnSeveralDefenders(t *testing.T) {
	b := boardOf(atk(1, 2, 2), atk(2, 2, 2), atk(3, 2, 2), def(1, 3, 3))
	b.Creatures.Set(301, Creature{Power: 1, Toughness: 1, Controller: 2})
	b.Life.Set(0, 20)
	b.Life.Set(1, 5)
	b.Life.Set(2, 20)
	d := decision.Decision{Seq: 1, Player: 0, Kind: decision.KAttackers, Max: 6}
	for _, id := range []state.ObjID{101, 102, 103} {
		for _, p := range []state.PlayerID{1, 2} {
			d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "attacker", Obj: id, Player: p})
		}
	}
	want := Decide(b, &d, rng(1)).Choices
	if got := AttackSimDecide(b, &d, rng(1), DefaultAttackSimParams()).Choices; !reflect.DeepEqual(got, want) {
		t.Fatalf("attack-sim = %v, want the default %v", got, want)
	}
}

// TestAttackSimLeavesOtherKindsAlone: a priority decision is answered by
// the default policy byte for byte.
func TestAttackSimLeavesOtherKindsAlone(t *testing.T) {
	b := boardOf(atk(1, 2, 2))
	b.Life.Set(0, 20)
	b.Life.Set(1, 20)
	d := decision.Decision{Seq: 3, Player: 0, Kind: decision.KPriority,
		Options: []decision.Option{{Index: 0, Kind: "pass"}}}
	if got, want := AttackSimDecide(b, &d, rng(7), DefaultAttackSimParams()), Decide(b, &d, rng(7)); !reflect.DeepEqual(got, want) {
		t.Fatalf("attack-sim = %+v, default = %+v", got, want)
	}
}

// TestLifeSimValueIsConcave pins the life table's shape: a point is worth
// more at low life than at high life, and the table is continuous at its
// knots.
func TestLifeSimValueIsConcave(t *testing.T) {
	const u = 9
	if lifeSimValue(10, u) != 210 || lifeSimValue(20, u) != 300 || lifeSimValue(1, u) != 30 {
		t.Fatalf("knots: %d %d %d", lifeSimValue(1, u), lifeSimValue(10, u), lifeSimValue(20, u))
	}
	low := lifeSimValue(5, u) - lifeSimValue(4, u)
	mid := lifeSimValue(15, u) - lifeSimValue(14, u)
	high := lifeSimValue(30, u) - lifeSimValue(29, u)
	if !(low > mid && mid > high) {
		t.Fatalf("marginal values %d, %d, %d are not decreasing", low, mid, high)
	}
}
