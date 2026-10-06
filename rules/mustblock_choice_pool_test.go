package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func TestCrashingBoarsMustBlockChoicePool(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	boarsCard, ok := reg.Lookup("Crashing Boars")
	if !ok {
		t.Fatal("corpus missing Crashing Boars")
	}
	e := combatEngine(t)
	boars := onBoardCard(t, e, 1, boarsCard)
	e.G.Obj(boars).SummonSick = false
	otherAttacker := onBoardReady(t, e, 1, "Name:Test Raider\nManaCost:1 R\nTypes:Creature Human\nPT:2/2\nOracle:x\n")
	chosen := onBoardReady(t, e, 0, "Name:Chosen Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	other := onBoardReady(t, e, 0, "Name:Other Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	tapped := onBoardCard(t, e, 0, card(t, "Name:Tapped Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"))
	e.G.Obj(tapped).Tapped = true
	foreign := onBoardReady(t, e, 1, "Name:Foreign Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	for _, id := range []state.ObjID{boars, otherAttacker, chosen, other, tapped, foreign} {
		if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
			t.Fatalf("precondition: object %d must be on battlefield: %+v", id, o)
		}
	}
	if !e.G.Obj(tapped).Tapped || e.G.Obj(chosen).Controller != 0 || e.G.Obj(foreign).Controller == 0 {
		t.Fatal("precondition: tapped status and defending-player control must differ")
	}

	e.G.Active = 1
	e.askAttackers()
	submitAttackersOnly(t, e, boars, otherAttacker)
	if len(e.G.Stack) != 1 {
		t.Fatalf("precondition: Boars attack trigger stack size = %d, want 1", len(e.G.Stack))
	}
	drainCombatPriority(t, e)
	if len(e.G.Stack) == 0 {
		t.Fatal("Crashing Boars trigger resolved without asking for its blocker choice")
	}
	e.resolveTop()
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("Crashing Boars did not ask for its blocker choice: %+v", d)
	}
	if d.Player != 0 {
		t.Fatalf("choice chooser = %d, want defending player 0", d.Player)
	}
	if len(d.Options) != 2 {
		t.Fatalf("choice pool = %+v, want exactly the two untapped defender-controlled creatures", d.Options)
	}
	var chooseIndex = -1
	for _, opt := range d.Options {
		if opt.Obj == chosen {
			chooseIndex = opt.Index
		}
		if opt.Obj == tapped || opt.Obj == foreign || opt.Obj == boars || opt.Obj == otherAttacker {
			t.Fatalf("ineligible object %d offered: %+v", opt.Obj, d.Options)
		}
	}
	if chooseIndex < 0 {
		t.Fatalf("chosen blocker %d absent from choices: %+v", chosen, d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{chooseIndex}}); err != nil {
		t.Fatalf("choose Crashing Boars blocker: %v", err)
	}
	if e.G.Obj(boars).Zone != state.ZBattlefield || !e.G.Obj(boars).IsAttacking {
		t.Fatalf("precondition: Crashing Boars must remain the attacking battlefield source: %+v", e.G.Obj(boars))
	}

	blockers := askBlockersFresh(t, e)
	if blockers == nil {
		t.Fatal("no blocker decision after selected creature was required")
	}
	boarsPair := findBlockOption(blockers, chosen, boars)
	otherPair := findBlockOption(blockers, chosen, otherAttacker)
	if boarsPair == nil || !boarsPair.Required || !boarsPair.BlockMust {
		t.Fatalf("selected blocker must carry the required MustBlock duty against Crashing Boars: %+v", blockers.Options)
	}
	if otherPair == nil || otherPair.BlockMust {
		t.Fatalf("selected blocker must not carry the MustBlock pair duty against the other attacker: %+v", blockers.Options)
	}
}
