package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/rules/combat"
	"github.com/adams-shaun/gorge/state"
)

// Independent one-attacker BlockAllDefined duties and an ordinary duty
// cannot combine into a multi-block permission for the same blocker.
func TestMustBlockMixedDutiesKeepPairPermission(t *testing.T) {
	e := threeSeatEngine(t)
	src := onBoard(t, e, 1, "Name:Duty source\nTypes:Creature\nPT:2/2\nSVar:All:DB$ MustBlock | ValidTgts$ Creature | DefinedAttacker$ TriggeredAttacker | BlockAllDefined$ True\nSVar:Ordinary:DB$ MustBlock | ValidTgts$ Creature | DefinedAttacker$ TriggeredAttacker\nOracle:x\n")
	blocker := onBoard(t, e, 0, "Name:Chosen blocker\nTypes:Creature\nPT:2/2\nOracle:x\n")
	a := onBoard(t, e, 1, "Name:All A\nTypes:Creature\nPT:2/2\nOracle:x\n")
	ordinary := onBoard(t, e, 1, "Name:Ordinary B\nTypes:Creature\nPT:2/2\nOracle:x\n")
	c := onBoard(t, e, 1, "Name:All C\nTypes:Creature\nPT:2/2\nOracle:x\n")
	attackSeat0(t, e, a, ordinary, c)
	face := e.G.Obj(src).Face()
	if e.G.Obj(blocker).Zone != state.ZBattlefield || a == ordinary || ordinary == c {
		t.Fatal("precondition: distinct attackers and battlefield blocker required")
	}
	for _, spec := range []struct {
		name     string
		attacker state.ObjID
	}{{"All", a}, {"Ordinary", ordinary}, {"All", c}} {
		sa := cards.ResolveSVar(face.SVars, spec.name)
		if sa == nil || !cards.MustBlockNamedTargetShape(sa) || !combat.CanBlock(asBoard(e), blocker, spec.attacker) {
			t.Fatalf("precondition: %s cannot bind legal pair %d", spec.name, spec.attacker)
		}
		effects.Resolve(e, &effects.Ctx{Source: src, Controller: 1, SVars: face.SVars, Remembered: []state.Target{{Obj: spec.attacker}}, Targets: []state.Target{{Obj: blocker}}, TargetsOffered: true}, sa)
	}
	b := asBoard(e)
	for _, attacker := range []state.ObjID{a, ordinary, c} {
		if !combat.MustBlockPairRequired(b, blocker, attacker) {
			t.Fatalf("precondition: no duty for attacker %d", attacker)
		}
	}
	for _, attacker := range []state.ObjID{a, ordinary, c} {
		if combat.MustBlockAllPair(b, blocker, attacker) {
			t.Fatalf("independent single-attacker duty granted multi-block permission for %d", attacker)
		}
	}
	d := askBlockersFresh(t, e)
	if d == nil || d.Kind != decision.KBlockers {
		t.Fatalf("precondition: missing blockers decision: %#v", d)
	}
	allA, ord, allC := findBlockOption(d, blocker, a), findBlockOption(d, blocker, ordinary), findBlockOption(d, blocker, c)
	if allA == nil || ord == nil || allC == nil || allA.BlockMustAll || ord.BlockMustAll || allC.BlockMustAll || d.GroupCapFor(allA.Group) != 1 {
		t.Fatalf("pair flags or cap wrong: %+v", d.Options)
	}
	if d.RequiredQuota() != 1 {
		t.Fatalf("satisfiable duty quota = %d, want 1", d.RequiredQuota())
	}
	for _, order := range [][]int{{allA.Index, allC.Index}, {ord.Index, allA.Index}, {allA.Index, ord.Index}} {
		in := decision.Intent{Seq: d.Seq, Player: d.Player, Choices: order}
		if err := d.Validate(in); err == nil {
			t.Fatalf("decision accepted mixed pairs %v", order)
		}
		if err := e.validateBlockers(d, in); err == nil {
			t.Fatalf("engine accepted mixed pairs %v", order)
		}
	}
	bot := newTestBot(7).answer(e, d)
	if err := d.Validate(bot); err != nil {
		t.Fatalf("bot answer rejected by decision: %v", err)
	}
	if err := e.validateBlockers(d, bot); err != nil {
		t.Fatalf("bot answer rejected by engine: %v", err)
	}
}
