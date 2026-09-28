package rules

import (
	"strconv"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

const goblinBowlingFixture = "Name:Fixture Goblin Bowling Team\nManaCost:1 R\nTypes:Creature Goblin\nPT:1/1\n" +
	"R:Event$ DamageDone | ActiveZones$ Battlefield | ValidSource$ Card.Self | ReplaceWith$ RollDamage | Description$ damage plus the roll\n" +
	"SVar:RollDamage:DB$ RollDice | ResultSVar$ Result | SubAbility$ DmgPlus\n" +
	"SVar:DmgPlus:DB$ ReplaceEffect | VarName$ DamageAmount | VarValue$ X\n" +
	"SVar:X:ReplaceCount$DamageAmount/Plus.Result\nOracle:x\n"

const hawkeyePlusYFixture = "Name:Fixture Hawkeye\nManaCost:3 R\nTypes:Legendary Creature Human Archer\nPT:2/4\n" +
	"R:Event$ DamageDone | ActiveZones$ Battlefield | ValidSource$ Card.YouCtrl | ValidTarget$ Permanent.OppCtrl,Opponent | IsCombat$ False | ReplaceWith$ DmgPlusX\n" +
	"SVar:DmgPlusX:DB$ ReplaceEffect | VarName$ DamageAmount | VarValue$ X\n" +
	"SVar:X:ReplaceCount$DamageAmount/Plus.Y\nSVar:Y:Count$CardPower\nOracle:x\n"

const dealDamageReplacementFixture = "Name:Fixture Damage Replacer\nManaCost:1 R\nTypes:Creature Goblin\nPT:1/1\n" +
	"R:Event$ DamageDone | ActiveZones$ Battlefield | ValidSource$ Card.Self | ReplaceWith$ DealThree\n" +
	"SVar:DealThree:DB$ DealDamage | Defined$ Opponent | NumDmg$ 3\nOracle:x\n"

func TestGoblinBowlingTeamDamagePlusRoll(t *testing.T) {
	t.Parallel()
	e, cfg, _ := etbConfig(t, seedTossSeat0(913), []string{goblinBowlingFixture}, nil)
	attacker := putCreature(t, e, 0, goblinBowlingFixture)
	if e.G.Obj(attacker) == nil || e.G.Obj(attacker).Zone != state.ZBattlefield {
		t.Fatal("Goblin Bowling fixture is not on the battlefield")
	}
	if e.Pending() == nil {
		e.Advance()
	}
	driveToStep(t, e, 3, 0, state.StepDeclareAttackers)
	if e.Pending() == nil {
		e.askAttackers()
	}
	submitAttackers(t, e, attacker)

	roll := int32(0)
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && strings.HasPrefix(ev.Text, "rolls a d6: ") {
			text := strings.TrimPrefix(ev.Text, "rolls a d6: ")
			if i := strings.IndexByte(text, ' '); i >= 0 {
				text = text[:i]
			}
			n, err := strconv.Atoi(text)
			if err != nil {
				t.Fatalf("cannot parse die roll %q: %v", ev.Text, err)
			}
			roll = int32(n)
			break
		}
	}
	if roll == 0 {
		t.Fatal("Goblin Bowling Team did not publish a d6 roll")
	}
	if roll == 1 {
		t.Fatalf("test seed produced roll %d, equal to the base-only comparison", roll)
	}
	want := int32(20) - 1 - roll
	if got := e.G.Players[1].Life; got != want {
		t.Fatalf("defender life = %d, want %d (1 base + die roll %d damage)", got, want, roll)
	}
	replayCheck(t, e, cfg)
}

func TestDealDamageReplacementStillUsesItsEmissions(t *testing.T) {
	t.Parallel()
	e, cfg, _ := etbConfig(t, seedTossSeat0(914), []string{dealDamageReplacementFixture}, nil)
	attacker := putCreature(t, e, 0, dealDamageReplacementFixture)
	if e.G.Obj(attacker) == nil || e.G.Obj(attacker).Zone != state.ZBattlefield {
		t.Fatal("DealDamage replacement fixture is not on the battlefield")
	}
	if e.Pending() == nil {
		e.Advance()
	}
	driveToStep(t, e, 3, 0, state.StepDeclareAttackers)
	if e.Pending() == nil {
		e.askAttackers()
	}
	submitAttackers(t, e, attacker)
	if got := e.G.Players[1].Life; got != 17 {
		t.Fatalf("defender life = %d, want 17 from replacement body's three damage", got)
	}
	replayCheck(t, e, cfg)
}

func TestHawkeyePlusYOperandApplied(t *testing.T) {
	t.Parallel()
	e, cfg, _ := etbConfig(t, seedTossSeat0(915), []string{hawkeyePlusYFixture, wgBoltSrc}, nil)
	hawkeye := putCreature(t, e, 0, hawkeyePlusYFixture)
	if e.G.Obj(hawkeye) == nil || e.G.Obj(hawkeye).Zone != state.ZBattlefield {
		t.Fatal("Hawkeye fixture is not on the battlefield")
	}
	power := e.Power(hawkeye)
	if power != 2 {
		t.Fatalf("Hawkeye power = %d, want 2 so the operand changes damage", power)
	}
	bolt := state.ObjID(0)
	for _, id := range e.G.Zone(state.ZHand, 0) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == "Fixture Bolt" {
			bolt = id
		}
	}
	if bolt == 0 {
		t.Fatal("Fixture Bolt is not in hand")
	}
	addMana(t, e, 0, "RRR")
	d := e.Pending()
	var castIndex = -1
	for _, option := range d.Options {
		if option.Kind == "cast" && option.Obj == bolt {
			castIndex = option.Index
		}
	}
	if castIndex < 0 {
		t.Fatalf("no cast option for Fixture Bolt: %+v", d.Options)
	}
	submitChoices(t, e, castIndex)
	submitTargetTo(t, e, 1)
	passUntilStackEmpty(t, e, 20)
	if got := e.G.Players[1].Life; got != 16 {
		t.Fatalf("opponent life = %d, want 16 (2 damage plus Hawkeye's 2 power)", got)
	}
	replayCheck(t, e, cfg)
}
