package botpolicy

import (
	"fmt"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// fact is one battlefield creature for the board helpers below: an object
// id and the facts the adapter halves would report for it.
type fact struct {
	id state.ObjID
	c  Creature
}

// boardOf builds a Board with the given creatures (Life empty unless a test
// sets it) -- the battlefield census the adapters would produce.
func boardOf(fs ...fact) Board {
	b := Board{Creatures: TableOf(map[state.ObjID]Creature{}), Life: TableOf(map[state.PlayerID]int32{})}
	for _, f := range fs {
		b.Creatures.Set(f.id, f.c)
	}
	return b
}

// atk is a fact helper: a creature controlled by seat 0 with the given
// power/toughness and keywords.
func atk(id int, p int32, t int32, kw ...string) fact {
	return fact{state.ObjID(100 + id), Creature{Power: p, Toughness: t, Keywords: kw, Controller: 0}}
}

// def is the same helper for seat 1.
func def(id int, p int32, t int32, kw ...string) fact {
	return fact{state.ObjID(200 + id), Creature{Power: p, Toughness: t, Keywords: kw, Controller: 1}}
}

// attackDecision builds a KAttackers decision for seat 0 (the attacker)
// against seat 1 with one option per object id, and returns the option
// indices Decide chose.
func attackDecision(b Board, ids ...int) []int {
	d := decision.Decision{Seq: 1, Player: 0, Kind: decision.KAttackers, Min: 0, Max: len(ids),
		Options: []decision.Option{}}
	for _, id := range ids {
		d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "attacker",
			Obj: state.ObjID(100 + id), Player: 1})
	}
	return Decide(b, &d, rng(1)).Choices
}

// blockDecision builds a KBlockers decision for seat 0 (the defender) with
// one option per (blocker, attacker) pair — ids 100+blocker, 200+attacker,
// matching the atk/def helpers — and returns the option indices Decide
// chose. An option's position in the returned slice is its position in
// pairs.
func blockDecision(b Board, pairs ...[2]int) []int {
	return blockDecisionFull(b, pairs...).choices
}

// blockDecisionFull is blockDecision plus the decision it answered, so a
// test can name which (blocker, attacker) a chosen option was.
type attackAnswer struct {
	d       *decision.Decision
	choices []int
}

func blockDecisionFull(b Board, pairs ...[2]int) attackAnswer {
	d := decision.Decision{Seq: 1, Player: 0, Kind: decision.KBlockers, Min: 0, Max: len(pairs),
		Options: []decision.Option{}}
	for _, p := range pairs {
		d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "block",
			Obj: state.ObjID(100 + p[0]), Attacker: state.ObjID(200 + p[1]), Player: 0})
	}
	return attackAnswer{&d, Decide(b, &d, rng(1)).Choices}
}

// TestAttackNothingToGain is AR1: a creature with no power deals no damage
// and trades nothing, so it stays home even with an empty defender board.
func TestAttackNothingToGain(t *testing.T) {
	b := boardOf(atk(1, 0, 4))
	if got := attackDecision(b, 1); len(got) != 0 {
		t.Errorf("0-power attacker chosen: %v, want none", got)
	}
}

// TestAttackUnblockable is AR2: a flying attacker against a defender with
// no Flying/Reach creature attacks — nothing can block it. A Reach 1/1
// makes it blockable, which hands the decision to the deadliest-block
// analysis (AR3) and then the leave-a-blocker rule (AR4): the 1/1 cannot
// kill the 3/3, but a Reach 4/4 can and does, and a 3/3 that trades for a
// reach 1/1 is held back to let that 1/1 die on the counter instead.
func TestAttackUnblockable(t *testing.T) {
	b := boardOf(atk(1, 3, 3, "Flying"))
	if got := attackDecision(b, 1); len(got) != 1 || got[0] != 0 {
		t.Errorf("flier vs no blockers = %v, want chosen", got)
	}
	b = boardOf(atk(1, 3, 3, "Flying"), def(1, 4, 4))
	if got := attackDecision(b, 1); len(got) != 1 {
		t.Errorf("flier vs ground-only defender = %v, want chosen", got)
	}
	b = boardOf(atk(1, 3, 3, "Flying"), def(1, 4, 4, "Reach"))
	if got := attackDecision(b, 1); len(got) != 0 {
		t.Errorf("flier vs reach 4/4 = %v, want none (it dies for free)", got)
	}
	// A lone 3/3-flier against a reach 1/1: swinging into a blocker that
	// trades for it is worse than holding it back, where the 1/1 has to
	// come to it instead (AR4 holds back the only attacker).
	b = boardOf(atk(1, 3, 3, "Flying"), def(1, 1, 1, "Reach"))
	if got := attackDecision(b, 1); len(got) != 0 {
		t.Errorf("flier vs reach 1/1 = %v, want it held back (AR4: it trades down)", got)
	}
}

// TestAttackCheapKillBlockIsDeadly is AR3: an attacker the defender can
// kill while losing less than the attacker is worth stays home. A 2/2
// swinging into a lone 3/3 loses to a free block; a 4/4 swinging into a
// 2/2 plus a 3/3 dies to the team for just the 2/2. Each case is also run
// beside a spare attacker that my side could swing, so it is AR3 alone
// that keeps the poisoned attacker home — not AR4 (which would hold the
// whole lone board back anyway).
func TestAttackCheapKillBlockIsDeadly(t *testing.T) {
	b := boardOf(atk(1, 2, 2), def(1, 3, 3))
	if got := attackDecision(b, 1); len(got) != 0 {
		t.Errorf("2/2 vs 3/3 = %v, want none (3/3 kills it for free)", got)
	}
	b = boardOf(atk(1, 4, 4), def(1, 2, 2), def(2, 3, 3))
	if got := attackDecision(b, 1); len(got) != 0 {
		t.Errorf("4/4 vs 2/2+3/3 = %v, want none (team kills it for a 2/2)", got)
	}
	// Spare-creature variants: the 5/5 attacks, the poisoned 2/2 stays
	// home, and AR4 has nothing to hold back (the 5/5 covers the board).
	b = boardOf(atk(1, 2, 2), atk(2, 5, 5), def(1, 3, 3))
	if got := attackDecision(b, 1, 2); len(got) != 1 || got[0] != 1 {
		t.Errorf("2/2+5/5 vs 3/3 = %v, want only the 5/5 to attack", got)
	}
	b = boardOf(atk(1, 4, 4), atk(2, 6, 6), def(1, 2, 2), def(2, 3, 3))
	if got := attackDecision(b, 1, 2); len(got) != 1 || got[0] != 1 {
		t.Errorf("4/4+6/6 vs 2/2+3/3 = %v, want only the 6/6 to attack", got)
	}
}

func TestLethalPressureCandidateForcesBlockOrWins(t *testing.T) {
	b := boardOf(atk(1, 5, 5), def(1, 6, 6))
	b.Life.Set(1, 5)
	d := decision.Decision{Seq: 1, Player: 0, Kind: decision.KAttackers, Min: 0, Max: 1,
		Options: []decision.Option{{Index: 0, Kind: "attacker", Obj: 101, Player: 1}}}
	// AR7 is promoted into the default: Decide and LethalPressureDecide both
	// swing the lethal-if-unblocked 5/5 into the 6/6.
	if got := Decide(b, &d, rng(1)).Choices; len(got) != 1 || got[0] != 0 {
		t.Fatalf("Decide choices = %v, want attack [0]", got)
	}
	if got := LethalPressureDecide(b, &d, rng(1)).Choices; len(got) != 1 || got[0] != 0 {
		t.Fatalf("LethalPressureDecide choices = %v, want attack [0]", got)
	}
	// Not lethal (6 life vs 5 power): the AR3 veto still holds the bad
	// trade back.
	b.Life.Set(1, 6)
	if got := Decide(b, &d, rng(1)).Choices; len(got) != 0 {
		t.Fatalf("non-lethal choices = %v, want the bad trade held back", got)
	}
}

func TestLethalPressureCandidateDoesNotAssumeMissingLifeIsZero(t *testing.T) {
	b := boardOf(atk(1, 5, 5), def(1, 6, 6))
	d := decision.Decision{Seq: 1, Player: 0, Kind: decision.KAttackers, Min: 0, Max: 1,
		Options: []decision.Option{{Index: 0, Kind: "attacker", Obj: 101, Player: 1}}}
	if got := LethalPressureDecide(b, &d, rng(1)).Choices; len(got) != 0 {
		t.Fatalf("missing defender life produced lethal-pressure attack %v", got)
	}
}

// TestAttackFirstStrikeSweeps pins the First-Strike branch of the damage
// simulation the combat math is built on (blockCombat): a First-Strike
// attacker kills the first blocker in its own step and the rest hit it
// one-for-one on the way through, so a 3/3-FS against two 2/2s survives
// the team block the ground version would die to — the kind of fact the
// bot would be guessing at if the keywords were not visible, and a rule
// the deadliest-block analysis must honour.
func TestAttackFirstStrikeSweeps(t *testing.T) {
	b := boardOf(atk(1, 3, 3, "First Strike"), atk(2, 1, 1), def(1, 2, 2), def(2, 2, 2))
	if got := attackDecision(b, 1, 2); len(got) != 1 || got[0] != 0 {
		t.Errorf("3/3-FS+1/1 vs two 2/2s = %v, want only the 3/3-FS to attack", got)
	}
	// The same attacker without First Strike is team-killed for one 2/2 and
	// stays home — the control that proves the keyword, not the stats, is
	// what earned the swing.
	b = boardOf(atk(1, 3, 3), atk(2, 1, 1), def(1, 2, 2), def(2, 2, 2))
	if got := attackDecision(b, 1, 2); len(got) != 0 {
		t.Errorf("3/3+1/1 vs two 2/2s = %v, want nothing to attack (team-killed)", got)
	}
}

// TestAttackTradesUpIsFine is AR3's other side: attacking is right when
// every way the defender can kill the attacker costs the defender at least
// the attacker's worth, or cannot kill it at all. A 4/4 into two 2/2s
// kills both; a 3/3 into a lone 2/2 the 2/2 cannot even kill; a 3/3 into
// a lone 3/3 trades at worst evenly. Each sits beside a 1/1 that the
// defender can kill for free and which stays home, so the board is never
// left completely unguarded and AR4 has nothing to hold back.
func TestAttackTradesUpIsFine(t *testing.T) {
	b := boardOf(atk(1, 4, 4), atk(2, 1, 1), def(1, 2, 2), def(2, 2, 2))
	if got := attackDecision(b, 1, 2); len(got) != 1 || got[0] != 0 {
		t.Errorf("4/4+1/1 vs two 2/2s = %v, want only the 4/4 to attack (it kills both)", got)
	}
	b = boardOf(atk(1, 3, 3), atk(2, 1, 1), def(1, 2, 2))
	if got := attackDecision(b, 1, 2); len(got) != 1 || got[0] != 0 {
		t.Errorf("3/3+1/1 vs lone 2/2 = %v, want only the 3/3 to attack (the 2/2 cannot kill it)", got)
	}
	b = boardOf(atk(1, 3, 3), atk(2, 1, 1), def(1, 3, 3))
	if got := attackDecision(b, 1, 2); len(got) != 1 || got[0] != 0 {
		t.Errorf("3/3+1/1 vs lone 3/3 = %v, want only the 3/3 to attack (even trade)", got)
	}
}

// TestAttackLeaveABlocker is AR4: an attack that would leave the board
// with nobody to block with, while the defender has a creature of its own,
// holds back the best blockable attacker. An unblockable attacker is never
// the one held back — it is the way a defensive board still wins.
func TestAttackLeaveABlocker(t *testing.T) {
	// Two 2/2s, defender has a 2/2: attacking with both leaves nothing to
	// block with, so one stays home. Ties break on object id, so the
	// higher id attacks.
	b := boardOf(atk(1, 2, 2), atk(2, 2, 2), def(1, 2, 2))
	got := attackDecision(b, 1, 2)
	if len(got) != 1 || got[0] != 1 {
		t.Errorf("both 2/2s vs defender 2/2 = %v, want exactly the id-2 option (one held back)", got)
	}
	// A lone unblockable attacker is not held back even though attacking
	// leaves no blocker behind: nothing on the ground can block it.
	b = boardOf(atk(1, 3, 3, "Flying"), def(1, 2, 2))
	if got := attackDecision(b, 1); len(got) != 1 {
		t.Errorf("lone flier vs ground defender = %v, want it to attack (unblockable)", got)
	}
	// No defender creatures: nothing to hold the board undefended against.
	b = boardOf(atk(1, 2, 2), atk(2, 2, 2))
	if got := attackDecision(b, 1, 2); len(got) != 2 {
		t.Errorf("both 2/2s vs empty board = %v, want both to attack", got)
	}
	// A vigilant attacker stays untapped, so it still counts as a blocker
	// and nothing needs to be held back.
	b = boardOf(atk(1, 2, 2), atk(2, 2, 2, "Vigilance"), def(1, 2, 2))
	if got := attackDecision(b, 1, 2); len(got) != 2 {
		t.Errorf("2/2 + vigilant 2/2 vs defender 2/2 = %v, want both to attack", got)
	}
}

// TestBlockFavorableAndEvenTrades is BR1: a blocker blocks when it kills
// the attacker and survives the return swing, or trades even-or-up. A 4/4
// blocks a 3/3 and survives; a 3/3 takes an even trade with another 3/3;
// a 1/1 is never thrown at a 6/6 it cannot touch when no life is at stake.
func TestBlockFavorableAndEvenTrades(t *testing.T) {
	b := boardOf(atk(1, 4, 4), def(1, 3, 3))
	b.Life.Set(0, 20)
	if got := blockDecision(b, [2]int{1, 1}); len(got) != 1 {
		t.Errorf("4/4 vs 3/3 = %v, want the block (it survives, attacker dies)", got)
	}
	b = boardOf(atk(1, 3, 3), def(1, 3, 3))
	b.Life.Set(0, 20)
	if got := blockDecision(b, [2]int{1, 1}); len(got) != 1 {
		t.Errorf("3/3 vs 3/3 = %v, want the block (even trade)", got)
	}
	b = boardOf(atk(1, 1, 1), def(1, 6, 6))
	b.Life.Set(0, 20)
	if got := blockDecision(b, [2]int{1, 1}); len(got) != 0 {
		t.Errorf("1/1 vs 6/6 = %v, want no block (it dies for nothing at 20 life)", got)
	}
	// Trading down is refused too: a 5/5 already carrying 4 damage dies to a
	// 4/4 while killing it, a trade the blocker loses, so it stays home when
	// no life is at stake.
	b = boardOf(fact{state.ObjID(101), Creature{Power: 5, Toughness: 5, Damage: 4, Controller: 0}},
		def(1, 4, 4))
	b.Life.Set(0, 20)
	if got := blockDecision(b, [2]int{1, 1}); len(got) != 0 {
		t.Errorf("5/5-damaged vs 4/4 = %v, want no block (both die and it trades down)", got)
	}
}

// TestBlockSpendsTheCheapestKiller is BR1's spending rule: among the
// blockers that kill the attacker acceptably, the cheapest is spent. A
// 1/1 Deathtouch trading for a 4/4 beats giving up a whole 4/4 in the
// same block.
func TestBlockSpendsTheCheapestKiller(t *testing.T) {
	b := boardOf(atk(1, 4, 4), atk(2, 1, 1, "Deathtouch"), def(1, 4, 4))
	b.Life.Set(0, 20)
	got := blockDecision(b, [2]int{1, 1}, [2]int{2, 1}) // both options block the 4/4
	if len(got) != 1 || got[0] != 1 {
		t.Errorf("4/4 vs {4/4, 1/1-deathtouch} = %v, want only the 1/1 option spent", got)
	}
}

// TestBlockChumpOnlyForLethal is BR2: a blocker that dies without killing
// blocks only when the unblocked damage would otherwise take the defender
// to zero or below. The trample case saves only the blocker's own
// toughness, which is exactly what the life bookkeeping subtracts.
func TestBlockChumpOnlyForLethal(t *testing.T) {
	// 4 damage at 5 life: not lethal, the 1/1 stays home.
	b := boardOf(atk(1, 1, 1), def(1, 4, 4))
	b.Life.Set(0, 5)
	if got := blockDecision(b, [2]int{1, 1}); len(got) != 0 {
		t.Errorf("1/1 vs 4/4 at 5 life = %v, want no chump (not lethal)", got)
	}
	// The same block at 4 life: lethal, chump.
	b.Life.Set(0, 4)
	if got := blockDecision(b, [2]int{1, 1}); len(got) != 1 {
		t.Errorf("1/1 vs 4/4 at 4 life = %v, want the chump (lethal)", got)
	}
	// Trample: at 3 life the unblocked 4 is lethal, and a 1/1 chump saves
	// the only life it can (1); still the right block.
	b = boardOf(atk(1, 1, 1), def(1, 4, 4, "Trample"))
	b.Life.Set(0, 3)
	if got := blockDecision(b, [2]int{1, 1}); len(got) != 1 {
		t.Errorf("1/1 vs trampling 4/4 at 3 life = %v, want the chump", got)
	}
	// Non-lethal incoming total with two attackers: no chump on either.
	b = boardOf(atk(1, 1, 1), def(1, 2, 2), def(2, 2, 2))
	b.Life.Set(0, 5)
	if got := blockDecision(b, [2]int{1, 1}, [2]int{1, 2}); len(got) != 0 {
		t.Errorf("1/1 vs two 2/2s at 5 life = %v, want no chump", got)
	}
}

// TestBlockMenaceUsesZeroOrAtLeastTwoCreatures is BR5: one available blocker
// means no block, while a lethal Menace attack with two available blockers
// gets a legal two-creature declaration.
func TestBlockMenaceUsesZeroOrAtLeastTwoCreatures(t *testing.T) {
	b := boardOf(atk(1, 1, 1), atk(2, 1, 1), def(1, 3, 3, "Menace"))
	b.Life.Set(0, 2) // the menace attacker is lethal if left unblocked

	if got := blockDecision(b, [2]int{1, 1}); len(got) != 0 {
		t.Fatalf("one available blocker produced declaration %v, want no block", got)
	}
	if got := blockDecision(b, [2]int{1, 1}, [2]int{2, 1}); len(got) != 2 {
		t.Fatalf("two available blockers produced declaration %v, want both", got)
	}
}

// TestBlockNeverThrowsAway is BR1+BR2's floor: a blocker that cannot kill
// its attacker and whose sacrifice saves no life is never spent, and the
// same blocker is never given to two attackers. A 2/2 against a {5/5,
// 1/1} pair blocks only the 1/1 (which it kills and survives) — the 5/5
// gets through untouched.
func TestBlockNeverThrowsAway(t *testing.T) {
	b := boardOf(atk(1, 2, 2), def(1, 5, 5), def(2, 1, 1))
	b.Life.Set(0, 20)
	got := blockDecision(b, [2]int{1, 1}, [2]int{1, 2}) // options: (2/2,5/5), (2/2,1/1)
	if len(got) != 1 {
		t.Fatalf("2/2 vs {5/5, 1/1} = %v, want exactly one block", got)
	}
	if got[0] != 1 {
		t.Errorf("2/2 blocked option %d, want option 1 (the 1/1 attacker, which it kills and survives)", got[0])
	}
}

// TestBlockCombatIsDeterministic pins the two determinism properties the
// combat branches must keep: the same decision with the same board answers
// identically every time, and with one 4/4 blocker against a 4/4 and a
// 3/3 attacker the single block always goes to the biggest threat (the
// 4/4), whichever order the options arrive in — only the option index
// that names it shifts.
func TestBlockCombatIsDeterministic(t *testing.T) {
	b := boardOf(atk(1, 4, 4), atk(2, 3, 3), def(1, 4, 4))
	b.Life.Set(0, 20)
	// Forward: option 0 is (blocker, 4/4 attacker).
	fa := blockDecisionFull(b, [2]int{1, 1}, [2]int{1, 2})
	if len(fa.choices) != 1 {
		t.Fatalf("want exactly one block with one blocker, got %v", fa.choices)
	}
	if fa.d.Options[fa.choices[0]].Attacker != state.ObjID(201) {
		t.Errorf("forward block = %+v, want the 4/4 attacker blocked", fa.d.Options[fa.choices[0]])
	}
	// Backward: option 1 is now (blocker, 4/4 attacker); same fact choice.
	ba := blockDecisionFull(b, [2]int{1, 2}, [2]int{1, 1})
	if len(ba.choices) != 1 {
		t.Fatalf("want exactly one block, got %v", ba.choices)
	}
	if ba.d.Options[ba.choices[0]].Attacker != state.ObjID(201) {
		t.Errorf("backward block = %+v, want the 4/4 attacker blocked", ba.d.Options[ba.choices[0]])
	}
	// Same input twice: identical output.
	same := blockDecisionFull(b, [2]int{1, 1}, [2]int{1, 2})
	for i := range fa.choices {
		if fa.choices[i] != same.choices[i] {
			t.Fatalf("same input gave different choices: %v vs %v", fa.choices, same.choices)
		}
	}
}

// TestCombatShapeTotality is the totality guard for the combat branches:
// option lists referencing objects with no board facts at all must answer
// without panicking and validate, the same contract
// TestTotalityUnderArbitraryMinMax holds for Decide as a whole — and a
// factless attacker stays home while a factless blocker never blocks.
func TestCombatShapeTotality(t *testing.T) {
	b := boardOf()
	att := decision.Decision{Seq: 1, Player: 0, Kind: decision.KAttackers, Min: 0, Max: 3,
		Options: []decision.Option{
			{Index: 0, Kind: "attacker", Obj: 0},
			{Index: 1, Kind: "attacker", Obj: 101},
			{Index: 2, Kind: "attacker", Obj: 102},
		}}
	in := Decide(b, &att, rng(1))
	if err := att.Validate(in); err != nil {
		t.Errorf("attackers intent %+v failed Validate: %v", in, err)
	}
	if len(in.Choices) != 0 {
		t.Errorf("factless attackers chosen: %v, want none (they read as 0 power)", in.Choices)
	}
	blk := decision.Decision{Seq: 2, Player: 0, Kind: decision.KBlockers, Min: 0, Max: 4,
		Options: []decision.Option{
			{Index: 0, Kind: "block", Obj: 101, Attacker: 201},
			{Index: 1, Kind: "block", Obj: 101, Attacker: 202},
			{Index: 2, Kind: "block", Obj: 102, Attacker: 201},
			{Index: 3, Kind: "block", Obj: 103, Attacker: 203},
		}}
	in = Decide(b, &blk, rng(1))
	if err := blk.Validate(in); err != nil {
		t.Errorf("blockers intent %+v failed Validate: %v", in, err)
	}
	if len(in.Choices) != 0 {
		t.Errorf("factless blockers chosen: %v, want none", in.Choices)
	}
}

// TestBoardFromGameCensusSkipsNonCreatures pins the game-shaped adapter's
// filter: the census must contain exactly the battlefield creatures and
// nothing else — a land sitting on the battlefield must not reach the
// combat heuristic as a blocker the seat could not actually use.
func TestBoardFromGameCensusSkipsNonCreatures(t *testing.T) {
	g := state.NewGame([]string{"a", "b"})
	whelp := g.AddObject(cardFace(t, "R Whelp", "Creature Whelp", 2, 2), 0)
	land := g.AddObject(cardFace(t, "Mountain", "Basic Land Mountain", 0, 0), 0)
	g.SetZone(state.ZBattlefield, 0, []state.ObjID{whelp.ID, land.ID})
	got := BoardFromGame(g, stubChars{}, 0)
	if got.Creatures.Len() != 1 {
		t.Fatalf("census has %d creatures, want 1 (the land must be dropped)", got.Creatures.Len())
	}
	cr := got.Creatures.Get(whelp.ID)
	if cr.Power != 2 || cr.Toughness != 2 {
		t.Errorf("creature facts = %d/%d, want 2/2", cr.Power, cr.Toughness)
	}
	if _, ok := got.Creatures.Lookup(land.ID); ok {
		t.Errorf("land object leaked into the creature census")
	}
	// A brand-new game sits in the untap step: not a main phase, and both
	// players at 20 life.
	if got.IsMain {
		t.Errorf("IsMain = true on a fresh game (step untap), want false")
	}
	if got.Life.Get(0) != 20 || got.Life.Get(1) != 20 {
		t.Errorf("life = %v, want 20/20", got.Life.Map())
	}
}

// stubChars is a Chars stand-in for the census test: everything reads as
// the object's printed 0/0 except object 1, which this test's source knows
// is the 2/2 Whelp — enough to prove the adapter derives the facts
// through the Chars it is handed rather than off the printed face.
type stubChars struct{}

func (stubChars) Power(id state.ObjID) int32 {
	if id == 1 {
		return 2
	}
	return 0
}
func (stubChars) Toughness(id state.ObjID) int32 {
	if id == 1 {
		return 2
	}
	return 0
}
func (stubChars) Keywords(id state.ObjID) []string { return nil }

func cardFace(t *testing.T, name, types string, p, tn int32) *cards.Card {
	t.Helper()
	src := fmt.Sprintf("Name:%s\nTypes:%s\nPT:%d/%d\nOracle:x\n", name, types, p, tn)
	c, diags := cards.ParseBytes("combat_test.go", []byte(src))
	if len(diags) != 0 {
		t.Fatalf("parsing %q: %v", src, diags)
	}
	c.Link()
	return c
}

// ---------------------------------------------------------------------------
// Task m34: the defender is a per-attacker choice (CR 506.2 / CR 903.14).

// defN is def for a defending seat of the caller's choosing (the atk/def
// helpers hard-code seats 0 and 1): the same Creature shape, controller n,
// object ids in the 300+ range so they cannot collide with atk/def's
// 100/200 bases.
func defN(n state.PlayerID, id int, p int32, t int32, kw ...string) fact {
	return fact{state.ObjID(300 + int(n)*100 + id), Creature{Power: p, Toughness: t, Keywords: kw, Controller: n}}
}

// attackDecisionDefs builds a KAttackers decision for seat 0 (the attacker)
// with one option per (defender, attacker id) pair -- the Task-m34 shape,
// where the same attacker may be offered against several defenders -- and
// returns the choices Decide made. The attacker facts come from atk() (ids
// 100+), the same base attackDecision uses.
func attackDecisionDefs(b Board, pairs ...[2]int) []int {
	return attackDecisionDefsFull(b, pairs...).choices
}

// attackDecisionDefsFull is attackDecisionDefs plus the decision it
// answered, so a test can name the defender a chosen option attacks.
// attackAnswer (blockDecisionFull's type) serves both.
func attackDecisionDefsFull(b Board, pairs ...[2]int) attackAnswer {
	d := decision.Decision{Seq: 1, Player: 0, Kind: decision.KAttackers, Min: 0, Max: len(pairs),
		Options: []decision.Option{}}
	for _, p := range pairs {
		d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "attacker",
			Obj: state.ObjID(100 + p[1]), Player: state.PlayerID(p[0])})
	}
	return attackAnswer{&d, Decide(b, &d, rng(1)).Choices}
}

// TestAttackDefenderLifeTiebreak pins AR6's order: tier, lowest life, then
// first option. In particular a later, higher-numbered seat must win a life
// comparison, but low life must never override block risk or a closing clock
// in the production baseline.
func TestAttackDefenderLifeTiebreak(t *testing.T) {
	for _, tc := range []struct {
		name       string
		life1      int32
		life2      int32
		reverse    bool
		blocker    bool
		clock      bool
		wantOption int
	}{
		{name: "lower life beats seat and option", life1: 40, life2: 10, wantOption: 1},
		{name: "lower life beats reversed options", life1: 10, life2: 40, reverse: true, wantOption: 1},
		{name: "equal life keeps first option not lowest seat", life1: 20, life2: 20, reverse: true},
		{name: "missing life keeps first option", reverse: true},
		// AR7 (promoted into the default): the 4/4 is lethal to seat 1 at 1
		// life if unblocked, so it swings there and forces the trade rather
		// than taking the unblockable 4 at the 40-life seat.
		{name: "lethal pressure beats unblockable tier", life1: 1, life2: 40, blocker: true, wantOption: 0},
		{name: "unblockable tier beats low life", life1: 5, life2: 40, blocker: true, wantOption: 1},
		{name: "closing clock beats low life", life1: 40, life2: 1, blocker: true, clock: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := boardOf(atk(1, 4, 4), atk(2, 0, 2)) // AR4 cannot mask the choice
			if tc.life1 != 0 || tc.life2 != 0 {
				b.Life.Set(1, tc.life1)
				b.Life.Set(2, tc.life2)
			}
			if tc.blocker {
				f := defN(1, 1, 4, 4) // even trade (tier 1), vs empty seat 2 (tier 2)
				b.Creatures.Set(f.id, f.c)
			}
			if tc.clock {
				b.Commanders = TableOf(map[state.ObjID]Commander{101: zoneCmd(1, map[state.PlayerID]int32{1: 18})})
			}
			pairs := [][2]int{{1, 1}, {2, 1}}
			if tc.reverse {
				pairs[0], pairs[1] = pairs[1], pairs[0]
			}
			got := attackDecisionDefs(b, pairs...)
			if len(got) != 1 || got[0] != tc.wantOption {
				t.Fatalf("attack choices = %v, want [%d]", got, tc.wantOption)
			}
		})
	}
}

// TestAttackChoosesTheDefenderItCanHurt is AR6's carrier: with the same
// creature offered against two defenders, the policy attacks the defender
// where the swing is guaranteed (AR2, unblockable) rather than the one that
// kills it for free (AR3 veto). Offered only against the killer, the same
// creature stays home -- the veto is per defender, not a board-wide
// decision.
func TestAttackChoosesTheDefenderItCanHurt(t *testing.T) {
	// A 3/3 flier against seat 1 (a plain 4/4: unblockable, guaranteed
	// damage) and seat 2 (a Reach 4/4: it dies for free) swings at seat 1.
	// The 0/2 stand-in on my side stays home by AR1 and still counts as a
	// leave-a-blocker for AR4, so the hold-back rule cannot mask the
	// defender choice under test.
	b := boardOf(atk(1, 3, 3, "Flying"), atk(2, 0, 2), defN(1, 1, 4, 4), defN(2, 1, 4, 4, "Reach"))
	fa := attackDecisionDefsFull(b, [2]int{1, 1}, [2]int{2, 1})
	if len(fa.choices) != 1 {
		t.Fatalf("flier vs {ground 4/4, reach 4/4} = %v, want exactly one swing", fa.choices)
	}
	if got := fa.d.Options[fa.choices[0]].Player; got != 1 {
		t.Errorf("flier swung at seat %d, want 1 (the defender that cannot block it)", got)
	}
	// Only the reach defender offered: the flier stays home instead of
	// dying for free.
	b = boardOf(atk(1, 3, 3, "Flying"), defN(2, 1, 4, 4, "Reach"))
	if got := attackDecisionDefs(b, [2]int{2, 1}); len(got) != 0 {
		t.Errorf("flier vs lone reach 4/4 = %v, want none (it dies for free)", got)
	}
}

// TestAttackSplitsTwoAttackersAcrossTwoDefenders is the multiplayer
// behaviour Task m34 exists to enable, at the policy level: each attacker
// independently chooses the defender it can hurt most -- seat 1 takes the
// flier it cannot block, seat 2 takes the ground 4/4 nobody attends -- so
// one combat points at two opponents at once. The 0/1 on my side stays
// home by AR1 and keeps AR4 (leave a blocker) from firing, so the split is
// the defender choice alone.
func TestAttackSplitsTwoAttackersAcrossTwoDefenders(t *testing.T) {
	// my board: 3/3 flier, 4/4, and a 0/1 that is never an attacker (AR1)
	// but still blocks next turn. seat 1: a ground 4/4 (cannot block the
	// flier; trades even with the 4/4). seat 2: nothing.
	b := boardOf(atk(1, 3, 3, "Flying"), atk(2, 4, 4), atk(3, 0, 1), defN(1, 1, 4, 4))
	fa := attackDecisionDefsFull(b, [2]int{1, 1}, [2]int{1, 2}, [2]int{1, 3}, [2]int{2, 1}, [2]int{2, 2}, [2]int{2, 3})
	got := map[state.ObjID]state.PlayerID{}
	for _, c := range fa.choices {
		got[fa.d.Options[c].Obj] = fa.d.Options[c].Player
	}
	if len(got) != 2 || got[101] != 1 || got[102] != 2 {
		t.Errorf("split attack = %v, want flier(101)->seat 1 and 4/4(102)->seat 2; choices=%v",
			got, fa.choices)
	}
}

// TestAttackClockClosesAgainstOnlyOneDefender is AR5 against a real
// defender choice (the Task-m34 brief's open question about m37): the
// commander clock is per (commander, damaged player), so closesClock is
// read against the defender each option names. A commander pounding seat
// 1's clock swings at seat 1 even though seat 1 can kill it for free --
// the swing is a second-track win, and it outranks the unblockable swing
// at the empty seat 2 -- while the same commander at 16/21 (not closing)
// stays home from seat 1 and takes the safe swing at seat 2 instead.
func TestAttackClockClosesAgainstOnlyOneDefender(t *testing.T) {
	b := boardOf(atk(1, 4, 4), atk(2, 0, 2), defN(1, 1, 1, 1, "Deathtouch"))
	b.Commanders = TableOf(map[state.ObjID]Commander{101: zoneCmd(1, map[state.PlayerID]int32{1: 18})})
	fa := attackDecisionDefsFull(b, [2]int{1, 1}, [2]int{2, 1})
	if len(fa.choices) != 1 || fa.d.Options[fa.choices[0]].Player != 1 {
		t.Fatalf("closing commander vs {1/1 deathtouch, empty} = %v, want the seat-1 swing (18+4 closes)", fa.choices)
	}
	b.Commanders = TableOf(map[state.ObjID]Commander{101: zoneCmd(1, map[state.PlayerID]int32{1: 16})})
	fa = attackDecisionDefsFull(b, [2]int{1, 1}, [2]int{2, 1})
	if len(fa.choices) != 1 || fa.d.Options[fa.choices[0]].Player != 2 {
		t.Fatalf("non-closing commander vs {1/1 deathtouch, empty} = %v, want the seat-2 swing (16+4 does not close)", fa.choices)
	}
}

// ---------------------------------------------------------------------------
// AR8: combined-attacker lethal pressure (opt-in CombinedLethalDecide).

// combinedAttackDecisionFull is attackDecisionDefsFull answered by the AR8
// policy instead of the default one: seat 0 attacks seat 1, one option per
// attacker id, and the decision it answered is returned so a test can name
// the chosen options.
func combinedAttackDecisionFull(b Board, ids ...int) attackAnswer {
	d := decision.Decision{Seq: 1, Player: 0, Kind: decision.KAttackers, Min: 0, Max: len(ids),
		Options: []decision.Option{}}
	for _, id := range ids {
		d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "attacker",
			Obj: state.ObjID(100 + id), Player: 1})
	}
	return attackAnswer{&d, CombinedLethalDecide(b, &d, rng(1)).Choices}
}

// combinedAttackDecision is combinedAttackDecisionFull's choices alone.
func combinedAttackDecision(b Board, ids ...int) []int {
	return combinedAttackDecisionFull(b, ids...).choices
}

// TestCombinedLethalTwoThreesVsFiveLife is the brief's headline case: two
// 3/3s against 5 life with a lone 2/2 blocker. No single attacker is lethal
// (AR7 misses it) and AR4 would hold one 3/3 back to keep a blocker, so the
// baseline attacks with one; AR8's subset search sees 3+3=6 >= 5 through a
// blocking response that can absorb only one 3/3 and sends both.
func TestCombinedLethalTwoThreesVsFiveLife(t *testing.T) {
	b := boardOf(atk(1, 3, 3), atk(2, 3, 3), def(1, 2, 2))
	b.Life.Set(1, 5)
	if got := attackDecision(b, 1, 2); len(got) != 1 {
		t.Fatalf("baseline (AR7 only) choices = %v, want one attacker held back", got)
	}
	got := combinedAttackDecision(b, 1, 2)
	if len(got) != 2 {
		t.Fatalf("AR8 choices = %v, want both 3/3s to attack (combined 6 >= 5)", got)
	}
}

// TestCombinedLethalSevenLifeAddsNothing is the same board at 7 life: the
// combined 6 still falls short, so AR8 finds no lethal subset and the
// baseline (AR4 holds one back) is unchanged.
func TestCombinedLethalSevenLifeAddsNothing(t *testing.T) {
	b := boardOf(atk(1, 3, 3), atk(2, 3, 3), def(1, 2, 2))
	b.Life.Set(1, 7)
	base := attackDecision(b, 1, 2)
	got := combinedAttackDecision(b, 1, 2)
	if len(base) != 1 || len(got) != 1 {
		t.Fatalf("at 7 life: baseline=%v AR8=%v, want both to send exactly one (6 < 7)", base, got)
	}
}

// TestCombinedLethalFlyingIsUnblockable pins the blocking-response model:
// a Flying attacker with no reach blocker is never absorbed, so its power
// reaches the defender in full. Two 2/2s (one flying) against 3 life and a
// lone ground 2/2 blocker: the blocker may absorb only the ground 2/2, the
// flier's 2 plus... -- the flier is unblockable, so the through damage is
// 2 (flier) while the ground 2/2 is blocked. Directly assert the model so
// a future change that treats the flier as blockable fails here.
func TestCombinedLethalFlyingIsUnblockable(t *testing.T) {
	flier := ar8Attacker{id: 101, a: Creature{Power: 3, Toughness: 3, Keywords: []string{"Flying"}, Controller: 0}}
	ground := ar8Attacker{id: 102, a: Creature{Power: 2, Toughness: 2, Controller: 0}}
	groundBlocker := blocker{id: 201, c: Creature{Power: 2, Toughness: 2, Controller: 1}}
	if got := ar8DamageThrough([]ar8Attacker{flier, ground}, []blocker{groundBlocker}); got != 3 {
		t.Fatalf("damage through flier(3)+ground(2) vs ground blocker = %d, want 3 (only the ground 2/2 is blockable)", got)
	}
	// A Reach blocker that can keep the block absorbs the flier: greedy takes
	// the largest reachable power, so the flier's 3 is eaten and the ground
	// 2/2 gets through -- 2. A reach 2/2 could NOT (it dies without killing
	// the 3/3), so the 4/4 is the blocker that actually stops the flier.
	reachBlocker := blocker{id: 202, c: Creature{Power: 4, Toughness: 4, Keywords: []string{"Reach"}, Controller: 1}}
	if got := ar8DamageThrough([]ar8Attacker{flier, ground}, []blocker{reachBlocker}); got != 2 {
		t.Fatalf("damage through vs reach 4/4 = %d, want 2 (the 3/3 flier is absorbed)", got)
	}
}

// TestCombinedLethalFlyingSwingIsFound is the integration counterpart: a
// 3/3 flier and a 2/2 ground attacker against 4 life with a lone 5/5 ground
// blocker (it cannot block the flier). The blocker absorbs the 2/2, leaving
// the flier's 3 -- not lethal on its own and not found by AR7 (3 < 4), and
// no subset reaches 4, so AR8 adds nothing here; raising the flier to 4/4
// makes the lone flier AR7-lethal. The case proves the model never counts a
// ground blocker against a flier in the SET search (the 3/3+2/2 set must
// yield exactly 3 through, not 0).
func TestCombinedLethalFlyingSwingIsFound(t *testing.T) {
	b := boardOf(atk(1, 3, 3, "Flying"), atk(2, 2, 2), def(1, 5, 5))
	b.Life.Set(1, 4)
	if got := combinedAttackDecision(b, 1, 2); len(got) != 1 || got[0] != 0 {
		t.Fatalf("AR8 vs 4 life with ground-only blocker = %v, want only the flier (blocker absorbs the 2/2; 3 < 4)", got)
	}
	if got := ar8DamageThrough(
		[]ar8Attacker{{id: 101, a: Creature{Power: 3, Toughness: 3, Keywords: []string{"Flying"}, Controller: 0}},
			{id: 102, a: Creature{Power: 2, Toughness: 2, Controller: 0}}},
		[]blocker{{id: 201, c: Creature{Power: 5, Toughness: 5, Controller: 1}}},
	); got != 3 {
		t.Fatalf("5/5 ground blocker must not absorb the flier: through = %d, want 3", got)
	}
}

// TestCombinedLethalUnknownLifeIsNoChange: a missing life fact is unknown,
// never zero, so AR8 has no effect -- the choices equal the baseline's.
func TestCombinedLethalUnknownLifeIsNoChange(t *testing.T) {
	b := boardOf(atk(1, 3, 3), atk(2, 3, 3), def(1, 2, 2))
	base := attackDecision(b, 1, 2)
	got := combinedAttackDecision(b, 1, 2)
	if len(got) != len(base) {
		t.Fatalf("unknown life: AR8=%v baseline=%v, want AR8 to add nothing", got, base)
	}
	for i := range got {
		if got[i] != base[i] {
			t.Fatalf("unknown life: AR8=%v baseline=%v, want identical", got, base)
		}
	}
}

// TestCombinedLethalDeterministic pins that two runs over the same board and
// decision produce byte-identical choices (no rng, no map-iteration order).
func TestCombinedLethalDeterministic(t *testing.T) {
	build := func() Board {
		b := boardOf(atk(1, 3, 3), atk(2, 3, 3), atk(3, 2, 2), def(1, 2, 2), def(2, 1, 1))
		b.Life.Set(1, 5)
		return b
	}
	a := combinedAttackDecision(build(), 1, 2, 3)
	c := combinedAttackDecision(build(), 1, 2, 3)
	if len(a) != len(c) {
		t.Fatalf("AR8 nondeterministic length: %v vs %v", a, c)
	}
	for i := range a {
		if a[i] != c[i] {
			t.Fatalf("AR8 nondeterministic choices: %v vs %v", a, c)
		}
	}
}

// TestCombinedLethalMultiDefenderDeterministic guards the defender-order rule:
// the same attacker may be offered against several defenders (CR 506.2), and
// AR8 processes the defenders in first-seen option order, not map-iteration
// order, so the forced assignment is a function of the offer list alone. Two
// runs over the same board and option list must be byte-identical.
func TestCombinedLethalMultiDefenderDeterministic(t *testing.T) {
	b := boardOf(atk(1, 3, 3), atk(2, 3, 3),
		defN(1, 1, 2, 2), defN(2, 1, 2, 2))
	b.Life.Set(1, 5)
	b.Life.Set(2, 5)
	build := func() decision.Decision {
		return decision.Decision{Seq: 1, Player: 0, Kind: decision.KAttackers, Min: 0, Max: 4,
			Options: []decision.Option{
				{Index: 0, Kind: "attacker", Obj: 101, Player: 1},
				{Index: 1, Kind: "attacker", Obj: 102, Player: 1},
				{Index: 2, Kind: "attacker", Obj: 101, Player: 2},
				{Index: 3, Kind: "attacker", Obj: 102, Player: 2},
			}}
	}
	d1, d2 := build(), build()
	a := CombinedLethalDecide(b, &d1, rng(1)).Choices
	c := CombinedLethalDecide(b, &d2, rng(1)).Choices
	if len(a) != len(c) {
		t.Fatalf("multi-defender AR8 nondeterministic length: %v vs %v", a, c)
	}
	for i := range a {
		if a[i] != c[i] {
			t.Fatalf("multi-defender AR8 nondeterministic choices: %v vs %v", a, c)
		}
	}
	if len(a) != 2 {
		t.Fatalf("multi-defender AR8 choices = %v, want both 3/3s forced (6 >= 5)", a)
	}
}
