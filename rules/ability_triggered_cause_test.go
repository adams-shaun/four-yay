package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// Provenance of the AbilityTriggered marker (Firebender Ascension): the
// causing object is the attacker the trigger line's OWN filter selected, not
// every declared attacker, and every push arm -- granted, keyword-granted --
// reports it, not only a printed Mode$ Attacks line.

// atBatchOther is an AttackersDeclared line that fires only for OTHER attackers.
const atBatchOther = "Name:Batch Other\nManaCost:1 R\nTypes:Creature Human\nPT:1/1\n" +
	"T:Mode$ AttackersDeclared | ValidAttackers$ Creature.Other+YouCtrl | Execute$ TrigGain | TriggerDescription$ x\n" +
	"SVar:TrigGain:DB$ GainLife | Defined$ You | LifeAmount$ 1\nOracle:x\n"

// atBatchSelf is an unfiltered AttackersDeclared line on the attacker itself.
const atBatchSelf = "Name:Batch Self\nManaCost:1 R\nTypes:Creature Human\nPT:1/1\n" +
	"T:Mode$ AttackersDeclared | Execute$ TrigGain | TriggerDescription$ x\n" +
	"SVar:TrigGain:DB$ GainLife | Defined$ You | LifeAmount$ 1\nOracle:x\n"

// atGrantAttack grants every creature you control a plain (non-keyword)
// "whenever this creature attacks" trigger through AddTrigger$.
const atGrantAttack = "Name:Banner of Cinders\nManaCost:0\nTypes:Enchantment\n" +
	"S:Mode$ Continuous | Affected$ Creature.YouCtrl | AddTrigger$ AtkGain | Description$ x\n" +
	"SVar:AtkGain:Mode$ Attacks | ValidCard$ Card.Self | Execute$ TrigGain | TriggerDescription$ x\n" +
	"SVar:TrigGain:DB$ GainLife | Defined$ You | LifeAmount$ 1\nOracle:x\n"

// atAttackOrdered declares attackers and answers every trigger-order ask with
// the options in order, or reversed, so both stack orders are exercised.
func atAttackOrdered(t *testing.T, e *Engine, reverse bool, ids ...state.ObjID) {
	t.Helper()
	e.askAttackers()
	submitAttackersOnly(t, e, ids...)
	for i := 0; i < 100; i++ {
		d := e.Pending()
		if d == nil || e.G.Step != state.StepDeclareAttackers {
			return
		}
		switch d.Kind {
		case decision.KTriggerOrder:
			picks := make([]int, len(d.Options))
			for j, o := range d.Options {
				k := j
				if reverse {
					k = len(d.Options) - 1 - j
				}
				picks[k] = o.Index
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: picks}); err != nil {
				t.Fatal(err)
			}
		case decision.KPriority:
			passPriorityOnce(t, e)
		default:
			t.Fatalf("unexpected decision %v", d.Kind)
		}
	}
	t.Fatal("attack drain exceeded its budget")
}

func TestFirebenderAscensionCountsOnlyTheAttackersOwnTriggerAmongSeveral(t *testing.T) {
	t.Parallel()
	for _, reverse := range []bool{false, true} {
		e, fb := atTable(t)
		archer := onBoardReady(t, e, 0, atArcher)
		watcher := onBoardReady(t, e, 0, atWatcherOther)
		life := e.G.Players[0].Life
		atAttackOrdered(t, e, reverse, archer, watcher)
		if e.G.Players[0].Life != life+2 {
			t.Fatalf("reverse=%v precondition: both attack triggers must resolve, life %d want %d", reverse, e.G.Players[0].Life, life+2)
		}
		// Archer's own trigger counts; Watcher's was caused by Archer.
		if got := e.G.Obj(fb).Counter("QUEST"); got != 1 {
			t.Fatalf("reverse=%v quest counters = %d, want 1", reverse, got)
		}
		if n, own := atMarkers(e); n != 2 || own != 1 {
			t.Fatalf("reverse=%v markers = %d (own %d), want 2 with 1 own", reverse, n, own)
		}
	}
}

func TestFirebenderAscensionIgnoresABatchTriggerForOtherAttackers(t *testing.T) {
	t.Parallel()
	for _, reverse := range []bool{false, true} {
		e, fb := atTable(t)
		plain := onBoardReady(t, e, 0, atPlain)
		watcher := onBoardReady(t, e, 0, atBatchOther)
		life := e.G.Players[0].Life
		atAttackOrdered(t, e, reverse, plain, watcher)
		if e.G.Players[0].Life != life+1 {
			t.Fatalf("reverse=%v precondition: the batch trigger must resolve, life %d want %d", reverse, e.G.Players[0].Life, life+1)
		}
		if got := e.G.Obj(fb).Counter("QUEST"); got != 0 {
			t.Fatalf("reverse=%v quest counters = %d, want 0 (the plain attacker has no trigger of its own)", reverse, got)
		}
		if n, own := atMarkers2(e, "AttackersDeclared"); n != 1 || own != 0 {
			t.Fatalf("reverse=%v batch markers = %d (own %d), want 1 not-own", reverse, n, own)
		}
	}
}

func TestFirebenderAscensionCountsAnUnfilteredBatchTriggerOfAnAttacker(t *testing.T) {
	t.Parallel()
	e, fb := atTable(t)
	self := onBoardReady(t, e, 0, atBatchSelf)
	life := e.G.Players[0].Life
	atAttack(t, e, self)
	if e.G.Players[0].Life != life+1 {
		t.Fatalf("precondition: the batch trigger must resolve, life %d want %d", e.G.Players[0].Life, life+1)
	}
	if got := e.G.Obj(fb).Counter("QUEST"); got != 1 {
		t.Fatalf("quest counters = %d, want 1", got)
	}
}

func TestFirebenderAscensionCountsAGrantedFirebendingAttackTrigger(t *testing.T) {
	t.Parallel()
	e, fb := atTable(t)
	onBoard(t, e, 0, grantedFirebendingStatic)
	bear := onBoardReady(t, e, 0, atPlain)
	if !e.HasKeyword(bear, "Firebending") {
		t.Fatal("precondition: the bear must hold the granted keyword")
	}
	atAttack(t, e, bear)
	pushes := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.KeywordTriggerPush && ev.Obj == bear {
			pushes++
		}
	}
	if pushes != 1 || e.G.Players[0].CombatMana[state.MR] != 3 {
		t.Fatalf("precondition: the granted trigger must resolve (pushes %d, mana %d)", pushes, e.G.Players[0].CombatMana[state.MR])
	}
	if got := e.G.Obj(fb).Counter("QUEST"); got != 1 {
		t.Fatalf("quest counters = %d, want 1", got)
	}
}

func TestFirebenderAscensionCountsAGrantedAttackTrigger(t *testing.T) {
	t.Parallel()
	e, fb := atTable(t)
	onBoard(t, e, 0, atGrantAttack)
	bear := onBoardReady(t, e, 0, atPlain)
	life := e.G.Players[0].Life
	atAttack(t, e, bear)
	grants := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.GrantTriggerPush && ev.Obj == bear {
			grants++
		}
	}
	if grants != 1 || e.G.Players[0].Life != life+1 {
		t.Fatalf("precondition: the AddTrigger$ attack trigger must resolve (pushes %d, life %d want %d)", grants, e.G.Players[0].Life, life+1)
	}
	if got := e.G.Obj(fb).Counter("QUEST"); got != 1 {
		t.Fatalf("quest counters = %d, want 1", got)
	}
}

func TestFirebenderAscensionIgnoresAnOpponentsAttackTrigger(t *testing.T) {
	t.Parallel()
	e, fb := atTable(t)
	archer := onBoardReady(t, e, 1, atArcher)
	e.G.Active = 1
	life := e.G.Players[1].Life
	atAttack(t, e, archer)
	if e.G.Players[1].Life != life+1 {
		t.Fatalf("precondition: the opponent's trigger must resolve, life %d want %d", e.G.Players[1].Life, life+1)
	}
	if got := e.G.Obj(fb).Counter("QUEST"); got != 0 {
		t.Fatalf("quest counters = %d, want 0", got)
	}
}

// atMarkers2 counts the AbilityTriggered markers a given causing mode left.
func atMarkers2(e *Engine, mode string) (n, own int) {
	for _, ev := range e.L.Events {
		if ev.Kind == events.AbilityTriggered && ev.Counter == mode {
			n++
			own += int(ev.Amount)
		}
	}
	return
}
