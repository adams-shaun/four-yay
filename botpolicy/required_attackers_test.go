package botpolicy

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// requiredDecision builds a KAttackers decision for seat 0 against seat 1
// with one option per id and per-option Required flags (the wire shape
// rules/combat.go's askAttackers emits since the goad marking), decides, and
// returns the chosen options' object ids.
func requiredDecision(t *testing.T, b Board, req map[int]bool, max int, ids ...int) []state.ObjID {
	t.Helper()
	d := decision.Decision{Seq: 1, Player: 0, Kind: decision.KAttackers, Min: 0, Max: max,
		Options: []decision.Option{}}
	for _, id := range ids {
		d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "attacker",
			Obj: state.ObjID(100 + id), Player: 1, Required: req[id]})
	}
	choices := Decide(b, &d, rng(1)).Choices
	var out []state.ObjID
	for _, c := range choices {
		out = append(out, d.Options[c].Obj)
	}
	return out
}

// TestChooseAttackersDeclaresRequiredAttackers pins the CR 508.1d half of the
// KAttackers policy: an option marked Required (a goaded creature, CR 701.38,
// or an unconditional MustAttack static -- the engine marks it on the option)
// is declared no matter what the value tiers say. The engine REJECTS a
// declaration that omits a required creature it could have included, so a
// policy without this rule produces an illegal intent -- measured on the
// commander bench (seed 1283): the bot declared around a goaded Knight of the
// White Orchid and the whole run aborted on "must attack with as many
// required creatures as possible".
func TestChooseAttackersDeclaresRequiredAttackers(t *testing.T) {
	// A required 0-power creature: AR1 (power <= 0 stays home) must not
	// swallow a requirement -- a goaded creature attacks even when it deals
	// no damage (it is a blocker magnet and a tapped-attacker cost at worst).
	b := boardOf(atk(1, 0, 2), atk(2, 3, 3), def(1, 2, 2))
	b.Life.Set(1, 20)
	got := requiredDecision(t, b, map[int]bool{1: true}, 2, 1, 2)
	for _, id := range got {
		if id == state.ObjID(101) {
			return
		}
	}
	t.Fatalf("the required 0-power creature was not declared: chose %v", got)
}

// TestChooseAttackersRequiredOverridesTheDeadlyBlockVeto pins AR3's
// subordination to the requirement: a goaded creature that every block kills
// for less than it is worth STILL attacks -- CR 508.1d requires attacking
// with as many required creatures as POSSIBLE, not as many as are ADVISABLE,
// and validateAttackDeclaration rejects the omission outright.
func TestChooseAttackersRequiredOverridesTheDeadlyBlockVeto(t *testing.T) {
	// The required 2/2 dies to the 2/1 for 3 < its 4 pt (an AR3 veto -- the
	// defender trades a 3-pt creature for a 4-pt one); the requirement
	// declares it anyway.
	b := boardOf(atk(1, 2, 2), def(1, 2, 1))
	b.Life.Set(1, 20)
	got := requiredDecision(t, b, map[int]bool{1: true}, 1, 1)
	if len(got) != 1 || got[0] != state.ObjID(101) {
		t.Fatalf("the vetoed required attacker was not declared: %v", got)
	}
}

// TestChooseAttackersRespectsTheWireMax pins the ceiling half: the engine
// exposes an AttackRestrict ceiling (CR 508.1j) as the decision's Max, and
// the policy must not answer with more options than Max. The required
// attackers were added first, so the truncation keeps requirements when the
// ceiling forces a choice between them (the engine requires only
// min(len(required), ceiling)).
func TestChooseAttackersRespectsTheWireMax(t *testing.T) {
	b := boardOf(atk(1, 2, 2), atk(2, 3, 3), def(1, 2, 2))
	b.Life.Set(1, 20)
	got := requiredDecision(t, b, map[int]bool{1: true}, 1, 1, 2)
	if len(got) != 1 {
		t.Fatalf("chose %d options against a Max of 1: %v", len(got), got)
	}
	if got[0] != state.ObjID(101) {
		t.Fatalf("the ceiling cut kept the wrong option: chose %v", got)
	}
}

// TestChooseAttackersKeepsRequiredUnderTheCeilingWhenRequiredIsSecond pins
// the ceiling cut against the OTHER option order: the decision's options
// follow the engine's (attacker, defender) enumeration, not the requirement
// list, so the REQUIRED attacker can be the LATER option. chosen accumulates
// in option first-seen order, so the cut must stably reorder required-first
// before truncating -- a plain cut kept the non-required attacker and dropped
// the required one, and validateAttackDeclaration rejected the whole
// declaration ("must attack with as many required creatures as possible":
// the seed-1283 run-abort class, reached through a MaxAttackers$ static
// coexisting with a goaded/MustAttack creature). Vigilance on both keeps
// AR4's hold-back from rescuing the cut.
func TestChooseAttackersKeepsRequiredUnderTheCeilingWhenRequiredIsSecond(t *testing.T) {
	b := boardOf(atk(1, 2, 2, "Vigilance"), atk(2, 3, 3, "Vigilance"), def(1, 2, 2))
	b.Life.Set(1, 20)
	got := requiredDecision(t, b, map[int]bool{2: true}, 1, 1, 2)
	if len(got) != 1 || got[0] != state.ObjID(102) {
		t.Fatalf("the ceiling cut dropped the required attacker (it was the later option): chose %v", got)
	}
}

// TestRequiredAttackerSkipsAVetoedDefenderForAScoreableOne pins AR3's scan
// half for a required attacker: a vetoed option must not END the option
// scan -- the remaining offered defenders are still scored, and only a
// requirement whose EVERY option is vetoed falls back to the first offered
// one. The old shape broke on the first vetoed option, so a goaded creature
// offered [a lethal seat-1 defender, a safe seat-2 chump] swung at the
// lethal defender and threw the safe swing away.
func TestRequiredAttackerSkipsAVetoedDefenderForAScoreableOne(t *testing.T) {
	b := boardOf(atk(1, 2, 2, "Vigilance"), def(1, 2, 1), defN(2, 1, 0, 4))
	b.Life.Set(1, 20)
	b.Life.Set(2, 20)
	d := decision.Decision{Seq: 1, Player: 0, Kind: decision.KAttackers, Min: 0, Max: 1,
		Options: []decision.Option{
			{Index: 0, Kind: "attacker", Obj: state.ObjID(101), Player: 1, Required: true},
			{Index: 1, Kind: "attacker", Obj: state.ObjID(101), Player: 2},
		}}
	got := Decide(b, &d, rng(1)).Choices
	if len(got) != 1 || d.Options[got[0]].Player != 2 {
		t.Fatalf("the required attacker swung at the vetoed defender: chose options %v", got)
	}
}

// TestPhyrexianPipPrefersTheLifePayment pins the KChoose mana-payment arm:
// for a phyrexian pip (and any pip ask carrying a life alternative), prefer
// "Pay 2 life" while the seat has life to spare -- pool mana is the scarcer
// resource because it is the only thing that can pay a generic pip, and
// spending it on a pip strands the generic (measured: seed 1295, Solphim's
// {1}{R/P}{R/P}, the bot paid its only R into a pip and the activation
// aborted with no progress every window). At life <= 5 the seat keeps the
// pool payment -- never walking itself to 0 by paying pips.
func TestPhyrexianPipPrefersTheLifePayment(t *testing.T) {
	ask := func(life int32, opts ...decision.Option) int {
		b := Board{Creatures: TableOf(map[state.ObjID]Creature{}), Life: TableOf(map[state.PlayerID]int32{0: life})}
		d := decision.Decision{Seq: 1, Player: 0, Kind: decision.KChoose, Min: 1, Max: 1,
			Options: opts}
		return Decide(b, &d, rng(1)).Choices[0]
	}
	both := []decision.Option{
		{Index: 0, Kind: "pay_R", Label: "Pay R", Amount: 1},
		{Index: 1, Kind: "pay_life", Label: "Pay 2 life", Amount: 2},
	}
	if got := ask(20, both...); got != 1 {
		t.Fatalf("at life 20 chose option %d, want the life payment (1)", got)
	}
	if got := ask(4, both...); got != 0 {
		t.Fatalf("at life 4 chose option %d, want the pool payment (0): two more life pips must not land the seat at 0", got)
	}
	lifeOnly := []decision.Option{{Index: 0, Kind: "pay_life", Label: "Pay 2 life", Amount: 2}}
	if got := ask(4, lifeOnly...); got != 0 {
		t.Fatalf("a life-only ask at life 4 chose option %d, want 0 (the only offer)", got)
	}
	colourOnly := []decision.Option{{Index: 0, Kind: "pay_R", Label: "Pay R", Amount: 1}}
	if got := ask(20, colourOnly...); got != 0 {
		t.Fatalf("a pool-only ask chose option %d, want 0", got)
	}
}
