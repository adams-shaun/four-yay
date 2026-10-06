package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/rules/combat"
	"github.com/adams-shaun/gorge/state"
)

// Avalanche Tusker's attack trigger is the implicit-source attacker grammar
// with a Creature.DefenderCtrl target and an UntilEndOfCombat duration, driven
// through a REAL attack declaration, trigger target ask and resolution rather
// than a hand-built Ctx: the target offer is limited to the defending
// player's creatures, the resolved duty binds the chosen creature to Tusker
// only, and the declare-blockers offer and validator agree on it.
func TestMustBlockCorpusAvalancheTuskerAttackTriggerFlow(t *testing.T) {
	e := threeSeatEngine(t)
	tusker := onBoardReadyCard(t, e, 1, mshCorpusCard(t, "Avalanche Tusker"))
	otherAttacker := onBoardReady(t, e, 1, "Name:Other attacker\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	chosen := onBoard(t, e, 0, "Name:Chosen defender\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	bystander := onBoard(t, e, 0, "Name:Defender bystander\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	thirdSeat := onBoard(t, e, 2, "Name:Third seat creature\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	ownSide := onBoard(t, e, 1, "Name:Attacker side creature\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	e.G.Active = 1
	e.G.Step = state.StepDeclareAttackers

	e.askAttackers()
	submitAttackersAt(t, e, [][2]state.ObjID{{tusker, 0}, {otherAttacker, 0}})

	// The trigger's target ask is posed to Tusker's controller.
	var d *decision.Decision
	for i := 0; i < 16; i++ {
		d = e.Pending()
		if d == nil || d.Kind != decision.KPriority {
			break
		}
		passPriority(t, e)
	}
	if d == nil || d.Kind == decision.KPriority || d.Kind == decision.KBlockers {
		t.Fatalf("precondition: Tusker's trigger posed no target ask: %+v", d)
	}
	offered := map[state.ObjID]int{}
	for _, o := range d.Options {
		offered[o.Obj] = o.Index
	}
	if _, ok := offered[chosen]; !ok {
		t.Fatalf("defending player's creature not offered: %+v", d.Options)
	}
	if _, ok := offered[bystander]; !ok {
		t.Fatalf("defending player's second creature not offered: %+v", d.Options)
	}
	for label, id := range map[string]state.ObjID{"third seat": thirdSeat, "attacker side": ownSide, "Tusker": tusker} {
		if _, ok := offered[id]; ok {
			t.Fatalf("Creature.DefenderCtrl offered the %s creature: %+v", label, d.Options)
		}
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{offered[chosen]}}); err != nil {
		t.Fatalf("submit Tusker target: %v", err)
	}
	drainCombatPriority(t, e)

	b := asBoard(e)
	if !e.G.Obj(tusker).IsAttacking || !e.G.Obj(otherAttacker).IsAttacking ||
		!combat.CanBlock(b, chosen, otherAttacker) || !combat.CanBlock(b, bystander, tusker) {
		t.Fatal("precondition: both attackers must be attacking and blockable")
	}
	if !combat.MustBlockPairRequired(b, chosen, tusker) || combat.MustBlockPairRequired(b, chosen, otherAttacker) ||
		combat.MustBlockCandidates(b, 0)[bystander] {
		t.Fatal("resolved duty is not scoped to the chosen creature and Tusker")
	}
	d = e.Pending()
	if d == nil || d.Kind != decision.KBlockers {
		t.Fatalf("precondition: no blockers decision after the trigger resolved: %+v", d)
	}
	pair, other, wrong := findBlockOption(d, chosen, tusker), findBlockOption(d, chosen, otherAttacker), findBlockOption(d, bystander, tusker)
	if pair == nil || other == nil || wrong == nil || !pair.Required || !pair.BlockMust ||
		other.Required || other.BlockMust || wrong.Required || d.RequiredQuota() != 1 {
		t.Fatalf("Tusker offered pairs incorrect: %+v", d.Options)
	}
	for _, choices := range [][]int{{}, {wrong.Index}, {other.Index}} {
		if err := e.validateBlockers(d, decision.Intent{Player: d.Player, Seq: d.Seq, Choices: choices}); err == nil {
			t.Fatalf("engine accepted declaration not obeying Tusker's duty: %v", choices)
		}
	}
	bot := newTestBot(7).answer(e, d)
	if err := d.Validate(bot); err != nil {
		t.Fatalf("bot answer rejected by decision: %v", err)
	}
	if err := e.validateBlockers(d, bot); err != nil {
		t.Fatalf("bot answer rejected by engine: %v", err)
	}
	if err := e.Submit(decision.Intent{Player: d.Player, Seq: d.Seq, Choices: []int{pair.Index}}); err != nil {
		t.Fatalf("required Tusker block rejected: %v", err)
	}
	if !blockCommitted(t, e, tusker, chosen) {
		t.Fatalf("Tusker block never committed (BlockedBy %v)", e.G.Obj(tusker).BlockedBy)
	}
}
