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

// Vortex Elemental has no DefinedAttacker$: Forge binds its source as the
// attacker. Pin that corpus form through the same offered-option and validator
// path used by a seat, not just the pair-duty helper.
func TestMustBlockCorpusVortexImplicitAttackerDeclaration(t *testing.T) {
	e := threeSeatEngine(t)
	vortex := onBoardCard(t, e, 1, mshCorpusCard(t, "Vortex Elemental"))
	blocker := onBoard(t, e, 0, "Name:Blocker\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	bystander := onBoard(t, e, 0, "Name:Bystander\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	otherAttacker := onBoard(t, e, 1, "Name:Other attacker\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	attackSeat0(t, e, vortex, otherAttacker)
	for label, id := range map[string]state.ObjID{"Vortex": vortex, "blocker": blocker, "bystander": bystander} {
		if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
			t.Fatalf("precondition: %s is not on battlefield", label)
		}
	}
	face := e.G.Obj(vortex).Face()
	var body *cards.SA
	for _, ab := range face.Abilities {
		if ab.API == "MustBlock" {
			body = ab
			break
		}
	}
	if body == nil || body.ParamStr(cards.PKDefinedAttacker) != "" {
		t.Fatalf("precondition: Vortex body = %#v; want implicit source attacker", body)
	}
	effects.Resolve(e, &effects.Ctx{Source: vortex, Controller: 1, SVars: face.SVars, Targets: []state.Target{{Obj: blocker}}, TargetsOffered: true}, body)
	b := asBoard(e)
	if !combat.MustBlockPairRequired(b, blocker, vortex) || combat.MustBlockPairRequired(b, blocker, otherAttacker) {
		t.Fatal("precondition/behavior: target duty is not scoped to Vortex Elemental")
	}
	if combat.MustBlockCandidates(b, 0)[bystander] {
		t.Fatal("unselected bystander acquired a blocking duty")
	}
	d := askBlockersFresh(t, e)
	if d == nil || d.Kind != decision.KBlockers {
		t.Fatalf("precondition: no blockers decision: %#v", d)
	}
	var required int = -1
	for i, opt := range d.Options {
		if opt.Obj == blocker && opt.Attacker == vortex {
			required = i
			if !opt.Required || !opt.BlockMust {
				t.Fatalf("offered required pair lacks flags: %+v", opt)
			}
		}
		if opt.Obj == blocker && opt.Attacker == otherAttacker && opt.BlockMust {
			t.Fatalf("unrelated pair marked required: %+v", opt)
		}
	}
	if required < 0 {
		t.Fatal("precondition: Vortex/blocker pair is not offered")
	}
	if err := e.validateBlockers(d, decision.Intent{Player: 0, Choices: []int{required}}); err != nil {
		t.Fatalf("validator rejected offered required declaration: %v", err)
	}
	if err := e.validateBlockers(d, decision.Intent{Player: 0}); err == nil {
		t.Fatal("validator accepted declaration omitting the required Vortex block")
	}
}

func TestMustBlockUnknownDefinedSelectorFailsClosed(t *testing.T) {
	e := threeSeatEngine(t)
	source := onBoard(t, e, 0, "Name:Source\nTypes:Creature\nPT:2/2\nOracle:x\n")
	blocker := onBoard(t, e, 1, "Name:Blocker\nTypes:Creature\nPT:2/2\nOracle:x\n")
	attacker := onBoard(t, e, 0, "Name:Attacker\nTypes:Creature\nPT:2/2\nOracle:x\n")
	e.G.Obj(attacker).IsAttacking, e.G.Obj(attacker).Attacking = true, 1
	card, err := cards.ParseBytes("unknown-mustblock.txt", []byte("Name:Unknown\nTypes:Sorcery\nA:DB$ MustBlock | Defined$ Bogus\nOracle:x\n"))
	if err != nil {
		t.Fatal(err)
	}
	card.Link()
	face := card.Faces[0]
	sa := face.Abilities[0]
	if cards.MustBlockNamedTargetShape(sa) {
		t.Fatal("precondition: unknown Defined$ selector was certified")
	}
	effects.Resolve(e, &effects.Ctx{Source: source, Controller: 0, SVars: face.SVars, Targets: []state.Target{{Obj: blocker}}, TargetsOffered: true}, sa)
	if combat.MustBlockCandidates(asBoard(e), 1)[blocker] || combat.MustBlockPairRequired(asBoard(e), blocker, attacker) {
		t.Fatal("unknown selector installed a blocking duty")
	}
	foundNote := false
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && ev.Text == "MustBlock selector shape unimplemented" {
			foundNote = true
		}
	}
	if !foundNote {
		t.Fatal("precondition: fail-closed MustBlock handler did not emit its diagnostic Note")
	}
}
