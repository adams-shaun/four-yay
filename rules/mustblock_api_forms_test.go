package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/rules/combat"
	"github.com/adams-shaun/gorge/state"
)

func TestMustBlockCorpusVortexImplicitSourceAttacker(t *testing.T) {
	e := threeSeatEngine(t)
	source := onBoardCard(t, e, 0, mshCorpusCard(t, "Vortex Elemental"))
	blocker := onBoard(t, e, 1, "Name:Blocker\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	bystander := onBoard(t, e, 1, "Name:Bystander\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	src, bo := e.G.Obj(source), e.G.Obj(blocker)
	if src == nil || src.Zone != state.ZBattlefield || bo == nil || bo.Zone != state.ZBattlefield {
		t.Fatal("precondition: source and target blocker must be on the battlefield")
	}
	src.IsAttacking, src.Attacking = true, 1
	if !combat.CanBlock(asBoard(e), blocker, source) {
		t.Fatal("precondition: target must be able to block Vortex Elemental")
	}
	var mustBlock = false
	for _, sa := range src.Face().Abilities {
		if sa != nil && sa.API == "MustBlock" {
			effects.Resolve(e, &effects.Ctx{Source: source, Controller: 0, SVars: src.Face().SVars,
				Targets: []state.Target{{Obj: blocker}}, TargetsOffered: true}, sa)
			mustBlock = true
			break
		}
	}
	if !mustBlock {
		t.Fatal("precondition: Vortex Elemental corpus MustBlock ability missing")
	}
	b := asBoard(e)
	if !combat.MustBlockCandidates(b, 1)[blocker] || combat.MustBlockCandidates(b, 1)[bystander] {
		t.Fatal("MustBlock did not bind only the selected blocker")
	}
	if !combat.MustBlockPairRequired(b, blocker, source) || combat.MustBlockPairRequired(b, blocker, bystander) {
		t.Fatal("implicit-source MustBlock duty was not scoped to Vortex Elemental")
	}
}
