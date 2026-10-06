package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/rules/combat"
	"github.com/adams-shaun/gorge/state"
)

// Hunt Down's parent targets the attacker; the child chooses a DIFFERENT
// creature to block it. Neither the spell source nor the child's target is
// the DefinedAttacker$ ParentTarget referent.
func TestMustBlockCorpusHuntDownParentTargetAttacker(t *testing.T) {
	e := threeSeatEngine(t)
	spell := onBoardCard(t, e, 1, mshCorpusCard(t, "Hunt Down"))
	attacker := onBoard(t, e, 1, "Name:Parent target\nTypes:Creature\nPT:2/2\nOracle:x\n")
	unrelated := onBoard(t, e, 1, "Name:Unrelated attacker\nTypes:Creature\nPT:2/2\nOracle:x\n")
	blocker := onBoard(t, e, 0, "Name:Child target\nTypes:Creature\nPT:2/2\nOracle:x\n")
	bystander := onBoard(t, e, 0, "Name:Bystander\nTypes:Creature\nPT:2/2\nOracle:x\n")
	attackSeat0(t, e, attacker, unrelated)
	if spell == attacker || attacker == unrelated || blocker == bystander ||
		e.G.Obj(spell).Zone != state.ZBattlefield || e.G.Obj(attacker).Zone != state.ZBattlefield ||
		e.G.Obj(blocker).Zone != state.ZBattlefield || !e.G.Obj(attacker).IsAttacking ||
		!e.G.Obj(unrelated).IsAttacking || !combat.CanBlock(asBoard(e), blocker, attacker) ||
		!combat.CanBlock(asBoard(e), blocker, unrelated) || !combat.CanBlock(asBoard(e), bystander, attacker) {
		t.Fatal("precondition: distinct battlefield parent, child, and unrelated legal pairs required")
	}
	sa, face := mustBlockCorpusBody(t, e, spell, "DBMustBlock")
	if sa.ParamStr(cards.PKDefinedAttacker) != "ParentTarget" || sa.ParamStr(cards.PKValidTgts) != "Creature" {
		t.Fatalf("precondition: Hunt Down child selector changed: %+v", sa.Params)
	}
	// The parent link's target is Targets; the child's independent target is
	// PickedTargets. This is the same Ctx split Resolve uses for a sub-ability.
	effects.Resolve(e, &effects.Ctx{Source: spell, Controller: 1, SVars: face.SVars,
		Targets: []state.Target{{Obj: attacker}}, PickedTargets: []state.Target{{Obj: blocker}}, TargetsOffered: true}, sa)
	board := asBoard(e)
	if !combat.MustBlockPairRequired(board, blocker, attacker) || combat.MustBlockPairRequired(board, blocker, unrelated) ||
		combat.MustBlockCandidates(board, 0)[bystander] {
		t.Fatal("ParentTarget attacker or child blocker resolved to wrong object")
	}
	d := askBlockersFresh(t, e)
	if d == nil || d.Kind != decision.KBlockers {
		t.Fatalf("precondition: no blockers decision: %#v", d)
	}
	pair, other, wrong := findBlockOption(d, blocker, attacker), findBlockOption(d, blocker, unrelated), findBlockOption(d, bystander, attacker)
	if pair == nil || other == nil || wrong == nil || !pair.Required || !pair.BlockMust ||
		other.BlockMust || wrong.BlockMust || d.RequiredQuota() != 1 {
		t.Fatalf("ParentTarget offered pairs incorrect: %+v", d.Options)
	}
	in := decision.Intent{Player: d.Player, Seq: d.Seq, Choices: []int{pair.Index}}
	if err := d.Validate(in); err != nil {
		t.Fatalf("decision rejected parent pair: %v", err)
	}
	if err := e.validateBlockers(d, in); err != nil {
		t.Fatalf("engine rejected parent pair: %v", err)
	}
	for _, choices := range [][]int{{}, {wrong.Index}} {
		in.Choices = choices
		if err := e.validateBlockers(d, in); err == nil {
			t.Fatalf("engine accepted declaration without required blocker: %v", choices)
		}
	}
	// An ordinary MustBlock can be discharged by another legal attacker;
	// that does not make the unrelated pair a NAMED required pair.
	in.Choices = []int{other.Index}
	if err := d.Validate(in); err != nil {
		t.Fatalf("decision rejected ordinary alternative: %v", err)
	}
	if err := e.validateBlockers(d, in); err != nil {
		t.Fatalf("engine rejected ordinary alternative: %v", err)
	}
}
