package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/rules/combat"
	"github.com/adams-shaun/gorge/state"
)

// This is a verify-and-pin of behaviour already implemented by 0698da6eb,
// 25dbdf994, and 44fd958de; it adds no production behaviour. It resolves the
// real corpus SVar rather than constructing a synthetic copy. The focused
// fixture uses effects.Resolve with the trigger's remembered attacker instead
// of building a trigger harness, which independently pins the printed body
// and its attacker binding.
func TestMustBlockCorpusTolsimirForcesTargetBlock(t *testing.T) {
	e := threeSeatEngine(t)
	tolsimirCard := mshCorpusCard(t, "Tolsimir, Midnight's Light")
	tolsimir := onBoardCard(t, e, 0, tolsimirCard)
	wolf := onBoard(t, e, 0, "Name:Wolf Token\nTypes:Token Creature Wolf\nPT:2/2\nOracle:x\n")
	blocker := onBoard(t, e, 1, "Name:Opponent Bear\nTypes:Creature Bear\nPT:3/3\nOracle:x\n")
	otherBlocker := onBoard(t, e, 1, "Name:Other Opponent Bear\nTypes:Creature Bear\nPT:3/3\nOracle:x\n")
	otherAttacker := onBoard(t, e, 0, "Name:Other Wolf\nTypes:Creature Wolf\nPT:2/2\nOracle:x\n")

	for label, id := range map[string]state.ObjID{
		"Tolsimir": tolsimir, "Wolf": wolf, "blocker": blocker,
		"other blocker": otherBlocker, "other attacker": otherAttacker,
	} {
		if obj := e.G.Obj(id); obj == nil || obj.Zone != state.ZBattlefield {
			t.Fatalf("precondition: %s %d is not on the battlefield", label, id)
		}
	}
	// The two attackers are in combat against seat 1; asBoard reads these
	// combat facts for both CanBlock and the pair-specific requirement.
	for _, id := range []state.ObjID{wolf, otherAttacker} {
		o := e.G.Obj(id)
		o.IsAttacking = true
		o.Attacking = 1
	}
	if !combat.CanBlock(asBoard(e), blocker, wolf) {
		t.Fatal("precondition: opponent's creature cannot block the Wolf")
	}

	face := e.G.Obj(tolsimir).Face()
	sa := cards.ResolveSVar(face.SVars, "TrigForceBlock")
	if sa == nil {
		t.Fatal("precondition: corpus Tolsimir has no TrigForceBlock SVar")
	}
	if sa.API != "MustBlock" || sa.Params["DefinedAttacker"] != "TriggeredAttacker" {
		t.Fatalf("precondition: TrigForceBlock = API %q params %#v, want api:MustBlock with DefinedAttacker$ TriggeredAttacker", sa.API, sa.Params)
	}

	// Resolve the printed body with the same referents its real Attacks
	// trigger supplies: the Wolf is remembered and the selected blocker is
	// the target. This pins corpus-SVar delivery without new trigger machinery.
	e.G.Step = state.StepDeclareBlockers
	effects.Resolve(e, &effects.Ctx{
		Source: tolsimir, Controller: 0, SVars: face.SVars,
		Remembered: []state.Target{{Obj: wolf}},
		Targets:    []state.Target{{Obj: blocker}}, TargetsOffered: true,
	}, sa)

	b := asBoard(e)
	candidates := combat.MustBlockCandidates(b, 1)
	if !candidates[blocker] || candidates[otherBlocker] {
		t.Fatalf("precondition/behavior: MustBlock candidates = %#v, want only targeted blocker %d", candidates, blocker)
	}
	if !combat.MustBlockPairRequired(b, blocker, wolf) {
		t.Fatal("targeted blocker is not required to block the Wolf")
	}
	if combat.MustBlockPairRequired(b, blocker, otherAttacker) {
		t.Fatal("targeted blocker is wrongly required to block the other attacker")
	}
}
