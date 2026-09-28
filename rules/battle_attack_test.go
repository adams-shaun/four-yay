package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// This file closes the "cannot be attacked" half of AGENTS.md's (battle1)
// row: CR 310.7 makes a battle a legal defender for its protector's
// opponents, the declare-attackers step offers it, and combat damage dealt
// to it removes that many defense counters (CR 310.8a) through the ordinary
// Damage fold. The zero-defense SBA / CR 310.11 defeated arm is a sibling
// ticket's scope and is not touched here.

// battleAttackBoard seats a real corpus Battle Siege under seat 0, answers
// its CR 310.10 protector ask (option 0 = seat 1 in this four-seat game),
// places a 2/2 attacker under seat 0 that is ready to attack, and returns the
// engine, the battle's id, the attacker's id and the recorded protector. The
// battle is NOT seat 0's own protector, so seat 0 -- an opponent of the
// protector -- may legally attack it under CR 310.7.
func battleAttackBoard(t *testing.T) (*Engine, state.ObjID, state.ObjID, state.PlayerID) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	e, _, id := battleBoard(t, reg, "Invasion of Tolvada")
	protector := e.G.Obj(id).Protector
	atk := onBoardReady(t, e, 0, "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	e.G.Step = state.StepDeclareAttackers
	return e, id, atk, protector
}

// battleDefenderOption returns the pending declare-attackers option that
// names battle id, or nil.
func battleDefenderOption(d *decision.Decision, id state.ObjID) *decision.Option {
	if d == nil {
		return nil
	}
	for i := range d.Options {
		if d.Options[i].Battle == id {
			return &d.Options[i]
		}
	}
	return nil
}

// TestBattleIsOfferedAsAnAttackerOption is the CR 310.7 offer half: at the
// declare-attackers step the battle a player protects is offered as a legal
// defender for that player's opponents, carrying the battle id on the option
// so the declaration can be distinguished from an attack at the protector.
func TestBattleIsOfferedAsAnAttackerOption(t *testing.T) {
	t.Parallel()
	e, id, atk, protector := battleAttackBoard(t)

	// Preconditions: the battle is really on the battlefield with counters,
	// its protector is a specific seat, and the attacker is genuinely able to
	// attack (untapped, not summoning sick).
	o := e.G.Obj(id)
	if o == nil || o.Zone != state.ZBattlefield || o.Face() == nil || !o.Face().IsBattle() {
		t.Fatalf("precondition: battle must be a battlefield Battle, got %+v", o)
	}
	if got := o.Counter("DEFENSE"); got != 5 {
		t.Fatalf("precondition: battle defense %d, want 5", got)
	}
	if !o.ProtectorValid || o.Protector != protector {
		t.Fatalf("precondition: protector valid=%v seat=%d, want true/%d", o.ProtectorValid, o.Protector, protector)
	}
	if protector == e.G.Active {
		t.Fatalf("precondition: protector seat %d must differ from the active seat", protector)
	}
	a := e.G.Obj(atk)
	if a == nil || a.Zone != state.ZBattlefield || a.Tapped || a.SummonSick || a.Controller != e.G.Active {
		t.Fatalf("precondition: attacker must be a ready battlefield creature of the active seat, got %+v", a)
	}

	e.askAttackers()
	d := e.Pending()
	if d == nil || d.Kind != decision.KAttackers {
		t.Fatalf("no declare-attackers decision posed, got %+v", d)
	}
	opt := battleDefenderOption(d, id)
	if opt == nil {
		t.Fatalf("battle %d was not offered as a defender; options=%+v", id, d.Options)
	}
	if opt.Obj != atk {
		t.Fatalf("battle option names attacker %d, want %d", opt.Obj, atk)
	}
	if opt.Player != protector {
		t.Fatalf("battle option names defender seat %d, want the protector %d", opt.Player, protector)
	}
}

// TestProtectorCannotAttackOwnBattle guards the eligibility direction: the
// player protecting a battle is not one of the opponents CR 310.7 lets attack
// it, so when that player is the active player the battle is not offered.
func TestProtectorCannotAttackOwnBattle(t *testing.T) {
	t.Parallel()
	e, id, _, protector := battleAttackBoard(t)
	e.G.Active = protector
	// Give the protector a ready attacker so the refusal is about the
	// battle's protector relation, not the absence of any attacker.
	onBoardReady(t, e, protector, "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")

	opt := battleDefenderOption(e.Pending(), id) // nil; askAttackers not called
	if opt != nil {
		t.Fatalf("stale option before the ask: %+v", opt)
	}
	e.askAttackers()
	d := e.Pending()
	if d == nil || d.Kind != decision.KAttackers {
		t.Fatalf("no declare-attackers decision posed, got %+v", d)
	}
	if opt := battleDefenderOption(d, id); opt != nil {
		t.Fatalf("the protector was offered its own protected battle: %+v", opt)
	}
}

// TestBattleAttackDealsDefenseCounterDamage is the CR 310.7/310.8a damage
// half: declaring an attack at a battle and then resolving combat damage
// removes that many defense counters (through the ordinary Damage fold), and
// the protector takes no life loss.
func TestBattleAttackDealsDefenseCounterDamage(t *testing.T) {
	t.Parallel()
	e, id, atk, protector := battleAttackBoard(t)
	startDefense := e.G.Obj(id).Counter("DEFENSE")
	startLife := e.G.Players[protector].Life
	power := e.combatDamageAmount(atk)
	if power <= 0 || startDefense <= 0 {
		t.Fatalf("precondition: need positive power and defense, got power=%d defense=%d", power, startDefense)
	}

	e.askAttackers()
	d := e.Pending()
	if d == nil || d.Kind != decision.KAttackers {
		t.Fatalf("no declare-attackers decision posed, got %+v", d)
	}
	opt := battleDefenderOption(d, id)
	if opt == nil {
		t.Fatalf("precondition: battle not offered; options=%+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{opt.Index}}); err != nil {
		t.Fatalf("submit battle attack: %v", err)
	}

	// The declaration must be recorded against the battle, not only the
	// protector: the log carries the battle id in the event's Obj.
	found := false
	for _, ev := range e.L.Events {
		if ev.Kind == events.DeclareAttackers && ev.Obj == id {
			found = true
		}
	}
	if !found {
		t.Fatal("no DeclareAttackers event naming the battle id")
	}
	a := e.G.Obj(atk)
	if !a.IsAttacking || a.AttackingBattle != id || a.Attacking != protector {
		t.Fatalf("attacker state = attacking:%v battle:%d defender:%d, want true/%d/%d",
			a.IsAttacking, a.AttackingBattle, a.Attacking, id, protector)
	}

	e.dealCombatDamage()
	e.checkStateBased()

	got := e.G.Obj(id).Counter("DEFENSE")
	if got != startDefense-power {
		t.Fatalf("battle defense after combat damage = %d, want %d (%d - power %d)",
			got, startDefense-power, startDefense, power)
	}
	if e.G.Obj(id).Damage != 0 {
		t.Fatalf("battle marked %d damage; battle damage must convert to defense counters, not marked damage",
			e.G.Obj(id).Damage)
	}
	if life := e.G.Players[protector].Life; life != startLife {
		t.Fatalf("protector life moved from %d to %d; attacking a battle deals no damage to the protector",
			startLife, life)
	}
}

// TestAttackingPlayerStillDealsLifeDamage is the control: the same board
// with the attack declared at the protector (a player attack) still deals
// combat damage to their life, so the battle conversion cannot have broken
// the ordinary player path.
func TestAttackingPlayerStillDealsLifeDamage(t *testing.T) {
	t.Parallel()
	e, id, atk, protector := battleAttackBoard(t)
	startLife := e.G.Players[protector].Life

	e.askAttackers()
	d := e.Pending()
	if d == nil || d.Kind != decision.KAttackers {
		t.Fatalf("no declare-attackers decision posed, got %+v", d)
	}
	var playerOpt *decision.Option
	for i := range d.Options {
		if d.Options[i].Obj == atk && d.Options[i].Player == protector && d.Options[i].Battle == 0 {
			playerOpt = &d.Options[i]
			break
		}
	}
	if playerOpt == nil {
		t.Fatalf("precondition: no ordinary player attack option; options=%+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{playerOpt.Index}}); err != nil {
		t.Fatalf("submit player attack: %v", err)
	}
	if a := e.G.Obj(atk); a.AttackingBattle != 0 {
		t.Fatalf("player attack recorded battle %d, want 0", a.AttackingBattle)
	}
	e.dealCombatDamage()
	if life := e.G.Players[protector].Life; life >= startLife {
		t.Fatalf("protector life %d did not drop from %d on an ordinary player attack", life, startLife)
	}
	if got := e.G.Obj(id).Counter("DEFENSE"); got != 5 {
		t.Fatalf("player attack changed the battle's defense to %d, want 5", got)
	}
}
