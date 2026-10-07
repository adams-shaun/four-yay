package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// Mode$ AbilityTriggered (Firebender Ascension): ValidMode$ names the CAUSING
// trigger's mode, carried by the events.AbilityTriggered marker pushTrigger
// emits beside TriggerPush.

const atArcher = "Name:Archer\nManaCost:1 R\nTypes:Creature Human\nPT:1/1\n" +
	"T:Mode$ Attacks | ValidCard$ Card.Self | Execute$ TrigGain | TriggerDescription$ x\n" +
	"SVar:TrigGain:DB$ GainLife | Defined$ You | LifeAmount$ 1\nOracle:x\n"

const atPlain = "Name:Plain\nManaCost:1 R\nTypes:Creature Human\nPT:1/1\nOracle:x\n"

// atWatcherOther fires on ANOTHER creature attacking: the ability is not the
// attacker's own (TriggeredOwnAbility$ False in Forge's reading).
const atWatcherOther = "Name:Watcher\nManaCost:1 R\nTypes:Creature Human\nPT:1/1\n" +
	"T:Mode$ Attacks | ValidCard$ Creature.Other+YouCtrl | Execute$ TrigGain | TriggerDescription$ x\n" +
	"SVar:TrigGain:DB$ GainLife | Defined$ You | LifeAmount$ 1\nOracle:x\n"

// atCounterOnly counts a ChangesZone-caused ability, never an attack one.
const atCounterOnly = "Name:Zone Watcher\nManaCost:1 R\nTypes:Enchantment\n" +
	"T:Mode$ AbilityTriggered | TriggerZones$ Battlefield | ValidMode$ ChangesZone | Execute$ TrigPut | ValidSource$ Creature.YouCtrl | TriggeredOwnAbility$ True | TriggerDescription$ x\n" +
	"SVar:TrigPut:DB$ PutCounter | Defined$ Self | CounterType$ QUEST | CounterNum$ 1\nOracle:x\n"

func atTable(t *testing.T) (*Engine, state.ObjID) {
	t.Helper()
	e := New(Config{Seed: 717, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{mountainDeck(t, 40), mountainDeck(t, 40)}})
	fb := onBoardCard(t, e, 0, mshCorpusCard(t, "Firebender Ascension"))
	if o := e.G.Obj(fb); o.Zone != state.ZBattlefield || o.Counter("QUEST") != 0 {
		t.Fatalf("precondition: Firebender Ascension on battlefield with no quest counters, got zone %v", o.Zone)
	}
	e.G.Active = 0
	e.G.Step = state.StepDeclareAttackers
	return e, fb
}

func atAttack(t *testing.T, e *Engine, ids ...state.ObjID) {
	t.Helper()
	e.askAttackers()
	submitAttackersOnly(t, e, ids...)
	drainCombatPriority(t, e)
	for i := 0; i < 8 && len(e.G.Stack) > 0; i++ {
		passPriorityOnce(t, e)
	}
}

func atMarkers(e *Engine) (n, own int) {
	for _, ev := range e.L.Events {
		// Firebender's own counter trigger is an AbilityTriggered-caused push
		// too; only the Attacks-caused markers are under test.
		if ev.Kind == events.AbilityTriggered && ev.Counter == "Attacks" {
			n++
			own += int(ev.Amount)
		}
	}
	return
}

func TestFirebenderAscensionCountsAnAttackTriggerOfAnAttacker(t *testing.T) {
	t.Parallel()
	e, fb := atTable(t)
	archer := onBoardReady(t, e, 0, atArcher)
	life := e.G.Players[0].Life
	atAttack(t, e, archer)
	if e.G.Players[0].Life != life+1 {
		t.Fatalf("precondition: the archer's attack trigger must have resolved (life %d, want %d)", e.G.Players[0].Life, life+1)
	}
	if got := e.G.Obj(fb).Counter("QUEST"); got != 1 {
		t.Fatalf("quest counters = %d, want 1", got)
	}
	if n, own := atMarkers(e); n != 1 || own != 1 {
		t.Fatalf("AbilityTriggered markers = %d (own %d), want 1 own", n, own)
	}
}

func TestFirebenderAscensionIgnoresAnAttackerWithoutATrigger(t *testing.T) {
	t.Parallel()
	e, fb := atTable(t)
	plain := onBoardReady(t, e, 0, atPlain)
	atAttack(t, e, plain)
	if got := e.G.Obj(fb).Counter("QUEST"); got != 0 {
		t.Fatalf("quest counters = %d, want 0 (no triggered ability was caused)", got)
	}
}

func TestFirebenderAscensionIgnoresAnotherCreaturesAttackTrigger(t *testing.T) {
	t.Parallel()
	e, fb := atTable(t)
	plain := onBoardReady(t, e, 0, atPlain)
	watcher := onBoardReady(t, e, 0, atWatcherOther)
	life := e.G.Players[0].Life
	atAttack(t, e, plain)
	if e.G.Players[0].Life != life+1 || e.G.Obj(watcher).Zone != state.ZBattlefield {
		t.Fatalf("precondition: the watcher's trigger must have resolved (life %d, want %d)", e.G.Players[0].Life, life+1)
	}
	if n, own := atMarkers(e); n != 1 || own != 0 {
		t.Fatalf("markers = %d (own %d), want 1 not-own", n, own)
	}
	if got := e.G.Obj(fb).Counter("QUEST"); got != 0 {
		t.Fatalf("quest counters = %d, want 0 (the ability is the watcher's, caused by the plain attacker)", got)
	}
}

func TestAbilityTriggeredValidModeRejectsOtherModes(t *testing.T) {
	t.Parallel()
	e, fb := atTable(t)
	zw := onBoard(t, e, 0, atCounterOnly)
	archer := onBoardReady(t, e, 0, atArcher)
	atAttack(t, e, archer)
	if got := e.G.Obj(fb).Counter("QUEST"); got != 1 {
		t.Fatalf("precondition: Firebender must have counted the attack trigger, got %d", got)
	}
	if got := e.G.Obj(zw).Counter("QUEST"); got != 0 {
		t.Fatalf("ValidMode$ ChangesZone watcher counted an Attacks-caused ability: %d", got)
	}
}

// TestAbilityTriggeredCarriersCensus pins the corpus carriers of the mode.
// Only Firebender Ascension (ValidMode$/ValidSource$/TriggeredOwnAbility$) is
// fully modelled; the others also carry ValidDestination$ or
// ValidSpellAbility$ and fail closed in trigmatch.abilityTriggeredMatches.
func TestAbilityTriggeredCarriersCensus(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	want := map[string]bool{"Aboleth Spawn": true, "Firebender Ascension": true,
		"Historian's Boon": true, "Strict Proctor": true}
	got := map[string]bool{}
	for _, c := range reg.AllCards() {
		for _, f := range c.Faces {
			for _, tr := range f.Triggers {
				if tr.ModeKind() == cards.TriggerAbilityTriggered {
					got[strings.ReplaceAll(f.Name, "’", "'")] = true
				}
			}
		}
	}
	if len(got) != len(want) {
		t.Fatalf("carriers = %v, want %v", got, want)
	}
	for n := range want {
		if !got[n] {
			t.Fatalf("carrier %q missing from %v", n, got)
		}
	}
}
