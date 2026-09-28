package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

const planeswalkerAttacker = "Name:Memnite\nManaCost:0\nTypes:Artifact Creature Construct\nPT:4/4\nOracle:x\n"
const combatPlaneswalker = "Name:Test Walker\nManaCost:2 U\nTypes:Planeswalker\nLoyalty:5\nOracle:x\n"

func declareAtPlaneswalker(t *testing.T, e *Engine, attacker, pw state.ObjID) {
	t.Helper()
	e.askAttackers()
	d := e.Pending()
	if d == nil || d.Kind != decision.KAttackers {
		t.Fatalf("declare attackers decision = %+v", d)
	}
	var chosen []decision.Option
	for _, opt := range d.Options {
		if opt.Obj == attacker && opt.Battle == pw {
			chosen = append(chosen, opt)
		}
	}
	if len(chosen) != 1 {
		t.Fatalf("planeswalker attack option count = %d, want 1 (options: %+v)", len(chosen), d.Options)
	}
	e.finishAttackers(chosen, 0)
	if got := e.G.Obj(attacker).AttackingBattle; got != pw {
		t.Fatalf("attacking planeswalker = %d, want %d", got, pw)
	}
}

func TestCreatureCanAttackPlaneswalkerAndCombatDamageRemovesLoyalty(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	attacker := onBoardReady(t, e, 0, planeswalkerAttacker)
	pw := onBoard(t, e, 1, combatPlaneswalker)
	e.emit(events.Event{Kind: events.CounterChange, Obj: pw, Counter: "LOYALTY", Amount: 5})
	if o := e.G.Obj(pw); o == nil || o.Zone != state.ZBattlefield || o.Counter("LOYALTY") <= 0 {
		t.Fatalf("planeswalker precondition: %+v", o)
	}
	if o := e.G.Obj(attacker); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("attacker precondition: %+v", o)
	}
	declareAtPlaneswalker(t, e, attacker, pw)
	before := e.G.Obj(pw).Counter("LOYALTY")
	power := e.combatDamageAmount(attacker)
	if before != 5 || power != 4 || before == power {
		t.Fatalf("damage precondition: loyalty=%d power=%d; want distinct 5 and 4", before, power)
	}
	e.dealCombatDamage()
	if got := e.G.Obj(pw).Counter("LOYALTY"); got != 1 {
		t.Fatalf("planeswalker loyalty after combat damage = %d, want 1", got)
	}
}

func TestCombatDamageToPlaneswalkerAtZeroLoyaltyAppliesStateBasedAction(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	attacker := onBoardReady(t, e, 0, planeswalkerAttacker)
	pw := onBoard(t, e, 1, combatPlaneswalker)
	e.emit(events.Event{Kind: events.CounterChange, Obj: pw, Counter: "LOYALTY", Amount: 3})
	if o := e.G.Obj(pw); o == nil || o.Zone != state.ZBattlefield || o.Counter("LOYALTY") != 3 {
		t.Fatalf("planeswalker precondition: %+v", o)
	}
	power := e.combatDamageAmount(attacker)
	if power != 4 || power == e.G.Obj(pw).Counter("LOYALTY") {
		t.Fatalf("damage precondition: power=%d loyalty=%d, want distinct 4 and 3", power, e.G.Obj(pw).Counter("LOYALTY"))
	}
	declareAtPlaneswalker(t, e, attacker, pw)
	e.dealCombatDamage()
	e.checkStateBased()
	if o := e.G.Obj(pw); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("zero-loyalty planeswalker zone = %+v, want graveyard", o)
	}
}
