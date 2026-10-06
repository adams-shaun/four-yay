package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
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
	// The duty names the parent target: blocking the unrelated attacker
	// obeys no requirement while the named block was possible, so the
	// declaration is illegal (CR 509.1c) and that pair is not Required.
	if other.Required {
		t.Fatalf("pair against the unrelated attacker published Required: %+v", other)
	}
	in.Choices = []int{other.Index}
	if err := e.validateBlockers(d, in); err == nil {
		t.Fatal("engine accepted blocking the unrelated attacker instead of the parent target")
	}
}

// The same ParentTarget attacker binding through a REAL cast of corpus Hunt
// Down: the root Pump's cast-time target is the attacker, the DBMustBlock
// link's own target (asked at resolution) is the blocker. This pins the Ctx
// split the hand-built test above assumes against the live resolution walk.
func TestMustBlockCorpusHuntDownRealCastBindsParentAttacker(t *testing.T) {
	reg := freshCorpusRegistry(t, "h/hunt_down.txt", "g/grizzly_bears.txt", "f/forest.txt")
	e, _ := etbreplEngine(t, reg, "Hunt Down", "Grizzly Bears", "Grizzly Bears")
	attacker := searchMoveByName(t, e, "Grizzly Bears", state.ZBattlefield)
	unrelated := searchMoveByName(t, e, "Grizzly Bears", state.ZBattlefield)
	blocker := searchMoveByNameSeat(t, e, 1, "Grizzly Bears", state.ZBattlefield)
	bystander := searchMoveByNameSeat(t, e, 1, "Grizzly Bears", state.ZBattlefield)
	if attacker == unrelated || blocker == bystander || e.G.Obj(blocker).Controller != 1 || e.G.Obj(attacker).Controller != 0 {
		t.Fatal("precondition: two distinct seat-0 bears and two distinct seat-1 bears required")
	}
	spell := searchMoveByName(t, e, "Hunt Down", state.ZHand)
	addMana(t, e, 0, "G")
	submitChoices(t, e, castOptionFor(t, e, spell).Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("precondition: casting Hunt Down posed %+v, want the root target ask", d)
	}
	pick := -1
	for _, o := range d.Options {
		if o.Obj == attacker {
			pick = o.Index
		}
	}
	if pick < 0 {
		t.Fatalf("precondition: attacker-to-be not offered as Hunt Down's root target: %+v", d.Options)
	}
	submitChoices(t, e, pick)
	for i := 0; i < 20 && len(e.G.Stack) > 0; i++ {
		d = e.Pending()
		if d == nil {
			t.Fatal("no decision with Hunt Down on the stack")
		}
		if d.Kind == decision.KPriority {
			passPriority(t, e)
			continue
		}
		pick = -1
		for _, o := range d.Options {
			if o.Obj == blocker {
				pick = o.Index
			}
		}
		if pick < 0 {
			t.Fatalf("precondition: child blocker target not offered: %+v", d)
		}
		submitChoices(t, e, pick)
	}
	if len(e.G.Stack) != 0 {
		t.Fatal("precondition: Hunt Down did not finish resolving")
	}
	found := 0
	for _, ce := range e.active() {
		if ce.Restriction != "MustBlock" {
			continue
		}
		found++
		if ce.MustBlockAttacker != attacker || len(ce.Remembered) != 1 || ce.Remembered[0] != blocker || ce.MustBlockAllAttackers {
			t.Fatalf("Hunt Down duty bound wrong pair: attacker=%d remembered=%v all=%v (want %d / [%d])",
				ce.MustBlockAttacker, ce.Remembered, ce.MustBlockAllAttackers, attacker, blocker)
		}
	}
	if found != 1 {
		t.Fatalf("Hunt Down registered %d MustBlock duties, want 1", found)
	}
	for _, id := range []state.ObjID{attacker, unrelated} {
		e.G.Obj(id).SummonSick = false
	}
	e.emit(events.Event{Kind: events.DeclareAttackers, Player: 1, IDs: []state.ObjID{attacker, unrelated}})
	e.G.Step = state.StepDeclareBlockers
	b := asBoard(e)
	if !combat.MustBlockPairRequired(b, blocker, attacker) || combat.MustBlockPairRequired(b, blocker, unrelated) ||
		combat.MustBlockCandidates(b, 1)[bystander] {
		t.Fatal("real-cast Hunt Down duty not scoped to the parent target attacker and the chosen blocker")
	}
	bd := askBlockersFresh(t, e)
	if bd == nil {
		t.Fatal("precondition: no blockers decision")
	}
	pair, other := findBlockOption(bd, blocker, attacker), findBlockOption(bd, blocker, unrelated)
	if pair == nil || other == nil || !pair.Required || other.Required || bd.RequiredQuota() != 1 {
		t.Fatalf("real-cast Hunt Down offered pairs incorrect: %+v", bd.Options)
	}
	if err := e.validateBlockers(bd, decision.Intent{Player: bd.Player, Seq: bd.Seq, Choices: []int{pair.Index}}); err != nil {
		t.Fatalf("engine rejected the parent-target block: %v", err)
	}
	if err := e.validateBlockers(bd, decision.Intent{Player: bd.Player, Seq: bd.Seq, Choices: []int{other.Index}}); err == nil {
		t.Fatal("engine accepted blocking the unrelated attacker instead")
	}
}
