package botpolicy

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// swarmBoard is seat 0 with n identical attackers (ids 101..) against seat 1
// at the given life with the given defender facts.
func swarmBoard(n int, p, t int32, life int32, defs ...fact) Board {
	b := boardOf(defs...)
	for i := 1; i <= n; i++ {
		f := atk(i, p, t)
		b.Creatures.Set(f.id, f.c)
	}
	b.Life.Set(1, life)
	return b
}

func swarmIDs(n int) []int {
	ids := make([]int, n)
	for i := range ids {
		ids[i] = i + 1
	}
	return ids
}

// TestSwarmLethalAttacksThroughCheapBlockers is AR9's carrier, the cardfuzz
// shape (batch4 line 15: 1410 Human Soldier tokens facing three creatures
// and 21 life, never attacking for dozens of turns). Every 1/1 alone "dies
// for free" to a 2/2 (AR3), yet ten of them against three blockers and 5
// life are lethal however the defender blocks: it can stop at most three,
// so the smallest lethal set -- 3 + 5 = 8 attackers -- is forced to attack.
func TestSwarmLethalAttacksThroughCheapBlockers(t *testing.T) {
	b := swarmBoard(10, 1, 1, 5, def(1, 2, 2), def(2, 2, 2), def(3, 2, 2))
	got := attackDecision(b, swarmIDs(10)...)
	want := []int{0, 1, 2, 3, 4, 5, 6, 7}
	if len(got) != len(want) {
		t.Fatalf("attack choices = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("attack choices = %v, want %v", got, want)
		}
	}
}

// TestSwarmLethalNeedsGuaranteedDamage: one short of lethal under the
// defender's best blocks (10 - 3 = 7 < 8) is no swarm; AR3 still keeps every
// token home, as before AR9.
func TestSwarmLethalNeedsGuaranteedDamage(t *testing.T) {
	b := swarmBoard(10, 1, 1, 8, def(1, 2, 2), def(2, 2, 2), def(3, 2, 2))
	if got := attackDecision(b, swarmIDs(10)...); len(got) != 0 {
		t.Fatalf("attack choices = %v, want none (7 guaranteed damage vs 8 life)", got)
	}
}

// TestSwarmLethalIgnoresTappedDefenders: a tapped creature cannot block
// (CR 509.1a), so two tapped 2/2s leave one blocker and 10 - 1 = 9 damage
// reaches 8 life; the smallest lethal set is 1 + 8 = 9 attackers.
func TestSwarmLethalIgnoresTappedDefenders(t *testing.T) {
	b := swarmBoard(10, 1, 1, 8, def(1, 2, 2), def(2, 2, 2), def(3, 2, 2))
	for _, id := range []state.ObjID{202, 203} {
		c := b.Creatures.Get(id)
		c.Tapped = true
		b.Creatures.Set(id, c)
	}
	if got := attackDecision(b, swarmIDs(10)...); len(got) != 9 {
		t.Fatalf("attack choices = %v, want 9 attackers", got)
	}
}

// TestSwarmLethalBlocksTheBiggestAttackers: the defender stops its k
// biggest attackers, so the damage that counts is what is left after them.
// A 5/5 and three 1/1s against one blocker and 3 life: the blocker takes the
// 5/5 and exactly three 1/1s get through -- lethal with all four (AR7 alone
// would send only the 5/5 into the waiting 6/6). A 6-life defender survives
// the swarm, and nothing attacks.
func TestSwarmLethalBlocksTheBiggestAttackers(t *testing.T) {
	mk := func(life int32) Board {
		b := boardOf(atk(1, 5, 5), atk(2, 1, 1), atk(3, 1, 1), atk(4, 1, 1), def(1, 6, 6))
		b.Life.Set(1, life)
		return b
	}
	if got := attackDecision(mk(3), 1, 2, 3, 4); len(got) != 4 {
		t.Fatalf("3 life: attack choices = %v, want all four", got)
	}
	if got := attackDecision(mk(6), 1, 2, 3, 4); len(got) != 0 {
		t.Fatalf("6 life: attack choices = %v, want none", got)
	}
}

// TestAttackPlannerScalesToTokenArmies pins the memoised block-risk path on
// a board the size cardfuzz reaches: 3000 identical 1/1s against 3000
// untapped 1/1s at 20 life. No swarm is lethal (3000 blockers), every
// attacker is an even trade (tier 1), and AR4 holds exactly one back. Before
// the memo and killBlockCost's linear prefix scan this decision simulated
// every blocker prefix for every attacker -- cubic in the board -- and did
// not finish.
func TestAttackPlannerScalesToTokenArmies(t *testing.T) {
	const n = 3000
	b := swarmBoard(n, 1, 1, 20)
	for i := 1; i <= n; i++ {
		b.Creatures.Set(state.ObjID(10000+i), Creature{Power: 1, Toughness: 1, Controller: 1})
	}
	d := decision.Decision{Seq: 1, Player: 0, Kind: decision.KAttackers, Min: 0, Max: n}
	for i := 1; i <= n; i++ {
		d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "attacker",
			Obj: state.ObjID(100 + i), Player: 1})
	}
	got := Decide(b, &d, rng(1)).Choices
	if len(got) != n-1 {
		t.Fatalf("attackers = %d, want %d (all but AR4's one held back)", len(got), n-1)
	}
}
