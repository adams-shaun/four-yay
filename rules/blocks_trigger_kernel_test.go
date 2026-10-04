package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// TestGodsendBlocksOffersTheAttackerKernel pins the Remembered-is-the-ATTACKER
// mapping through Godsend's Blocks half (ChooseCard | DefinedCards$
// TriggeredAttackers): the choice pool holds exactly the blocked attacker and
// the answer exiles it.
func TestGodsendBlocksOffersTheAttackerKernel(t *testing.T) {
	t.Parallel()
	godsend := mshCorpusCardPath(t, "Godsend", "g/godsend.txt")
	e := blocksCombatEngine(t, 1)
	h := onBoard(t, e, 0, "Name:Kor Outfitter\nManaCost:1 W\nTypes:Creature Kor Cleric\nPT:2/2\nOracle:x\n")
	gw := onBoardCard(t, e, 0, godsend)
	e.G.Obj(gw).AttachedTo = h
	mem := onBoard(t, e, 1, "Name:Memnite\nManaCost:0\nTypes:Artifact Creature Construct\nPT:1/1\nOracle:x\n")
	e.G.Obj(mem).SummonSick = false

	e.askAttackers()
	submitAttackersOnly(t, e, mem)
	drainCombatPriority(t, e)
	if d := e.Pending(); d == nil || d.Kind != decision.KBlockers {
		t.Fatalf("expected a blockers decision, got %+v", d)
	}
	submitBlockersOnly(t, e, h)
	e.pending = nil
	e.resolveTop()
	if d := e.Pending(); d == nil || d.Kind != decision.KTriggerOptional {
		t.Fatalf("expected the OptionalDecider ask, got %+v", d)
	}
	submitChoices(t, e, 0)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("expected the ChooseCard ask, got %+v", d)
	}
	if len(d.Options) != 1 || d.Options[0].Obj != mem {
		t.Fatalf("choice pool = %+v, want exactly the ATTACKER %d", d.Options, mem)
	}
	submitChoices(t, e, d.Options[0].Index)
	if got := e.G.Obj(mem).Zone; got != state.ZExile {
		t.Fatalf("attacker zone = %s, want exile", got)
	}
}
