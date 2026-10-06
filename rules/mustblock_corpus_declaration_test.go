package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/rules/combat"
	"github.com/adams-shaun/gorge/state"
)

// Two separate Fighter Class triggers may each require the same creature to
// block, but neither grants permission to block more than one attacker.
// The maximum satisfiable duty count is one (CR 509.1a).
func TestMustBlockCorpusFighterClassSelectedTargetDeclaration(t *testing.T) {
	e := threeSeatEngine(t)
	class := onBoardCard(t, e, 1, mshCorpusCard(t, "Fighter Class"))
	blocker := onBoard(t, e, 0, "Name:Chosen blocker\nTypes:Creature\nPT:2/2\nOracle:x\n")
	bystander := onBoard(t, e, 0, "Name:Bystander\nTypes:Creature\nPT:2/2\nOracle:x\n")
	a := onBoard(t, e, 1, "Name:First attacker\nTypes:Creature\nPT:2/2\nOracle:x\n")
	b := onBoard(t, e, 1, "Name:Second attacker\nTypes:Creature\nPT:2/2\nOracle:x\n")
	attackSeat0(t, e, a, b)
	if a == b || blocker == bystander || e.G.Obj(class).Zone != state.ZBattlefield || e.G.Obj(blocker).Zone != state.ZBattlefield || e.G.Obj(bystander).Zone != state.ZBattlefield {
		t.Fatal("precondition: Class, distinct attackers and distinct blockers must be on battlefield")
	}
	sa, face := mustBlockCorpusBody(t, e, class, "TrigMustBlock")
	if !combat.CanBlock(asBoard(e), blocker, a) || !combat.CanBlock(asBoard(e), blocker, b) || !combat.CanBlock(asBoard(e), bystander, a) {
		t.Fatal("precondition: selected and unselected creatures must have legal block pairs")
	}
	for _, attacker := range []state.ObjID{a, b} {
		effects.Resolve(e, &effects.Ctx{Source: class, Controller: 1, SVars: face.SVars,
			Remembered: []state.Target{{Obj: attacker}}, Targets: []state.Target{{Obj: blocker}}, TargetsOffered: true}, sa)
	}
	board := asBoard(e)
	if !combat.MustBlockPairRequired(board, blocker, a) || !combat.MustBlockPairRequired(board, blocker, b) ||
		combat.MustBlockPairRequired(board, bystander, a) || combat.MustBlockCandidates(board, 0)[bystander] {
		t.Fatal("selected target's pair duties leaked to bystander or failed to bind")
	}
	d := askBlockersFresh(t, e)
	if d == nil || d.Kind != decision.KBlockers {
		t.Fatalf("precondition: missing blockers decision: %#v", d)
	}
	first, second := findBlockOption(d, blocker, a), findBlockOption(d, blocker, b)
	other := findBlockOption(d, bystander, a)
	if first == nil || second == nil || other == nil || !first.Required || !second.Required ||
		!first.BlockMust || !second.BlockMust || first.BlockMustAll || second.BlockMustAll ||
		other.BlockMust || other.Required || d.GroupCapFor(first.Group) != 1 {
		t.Fatalf("Fighter Class options/cap incorrect: %+v", d.Options)
	}
	team := d.BlockRequiredTeam()
	if len(team) != 1 || (team[0] != first.Index && team[0] != second.Index) || d.RequiredQuota() != 1 {
		t.Fatalf("decision required team = %v quota=%d, want one pair", team, d.RequiredQuota())
	}
	for _, chosen := range []int{first.Index, second.Index} {
		in := decision.Intent{Player: d.Player, Seq: d.Seq, Choices: []int{chosen}}
		if err := d.Validate(in); err != nil {
			t.Fatalf("decision rejected one selected pair: %v", err)
		}
		if err := e.validateBlockers(d, in); err != nil {
			t.Fatalf("engine rejected one selected pair: %v", err)
		}
	}
	both := decision.Intent{Player: d.Player, Seq: d.Seq, Choices: []int{first.Index, second.Index}}
	if err := d.Validate(both); err == nil {
		t.Fatal("decision accepted double block without multi-block permission")
	}
	if err := e.validateBlockers(d, both); err == nil {
		t.Fatal("engine accepted double block without multi-block permission")
	}
	for _, choices := range [][]int{{}, {other.Index}} {
		in := decision.Intent{Player: d.Player, Seq: d.Seq, Choices: choices}
		if err := e.validateBlockers(d, in); err == nil {
			t.Fatalf("engine accepted declaration omitting selected blocker: %v", choices)
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

// Magnetic Web chooses the marked creatures through Defined$ Valid, not the
// trigger's explicit target list. Its marked blocker has a named pair duty
// to the remembered attacker; no unmarked creature gains that duty. As an
// ordinary MustBlock, blocking another attacker can also satisfy it.
func TestMustBlockCorpusMagneticWebDefinedSetDeclaration(t *testing.T) {
	e := threeSeatEngine(t)
	web := onBoardCard(t, e, 0, mshCorpusCard(t, "Magnetic Web"))
	attacker := onBoard(t, e, 1, "Name:Marked attacker\nTypes:Creature\nPT:2/2\nOracle:x\n")
	otherAttacker := onBoard(t, e, 1, "Name:Unmarked attacker\nTypes:Creature\nPT:2/2\nOracle:x\n")
	blocker := onBoard(t, e, 0, "Name:Marked blocker\nTypes:Creature\nPT:2/2\nOracle:x\n")
	bystander := onBoard(t, e, 0, "Name:Unmarked blocker\nTypes:Creature\nPT:2/2\nOracle:x\n")
	attackSeat0(t, e, attacker, otherAttacker)
	for _, id := range []state.ObjID{attacker, blocker} {
		e.emit(events.Event{Kind: events.CounterChange, Obj: id, Counter: "MAGNET", Amount: 1})
	}
	if e.G.Obj(web).Zone != state.ZBattlefield || e.G.Obj(blocker).Zone != state.ZBattlefield ||
		e.G.Obj(bystander).Zone != state.ZBattlefield || e.G.Obj(attacker).Counter("MAGNET") == 0 ||
		e.G.Obj(blocker).Counter("MAGNET") == 0 || e.G.Obj(bystander).Counter("MAGNET") != 0 ||
		e.G.Obj(otherAttacker).Counter("MAGNET") != 0 || attacker == otherAttacker {
		t.Fatal("precondition: battlefield marked and unmarked creatures must differ")
	}
	sa, face := mustBlockCorpusBody(t, e, web, "TrigLure")
	if !combat.CanBlock(asBoard(e), blocker, attacker) || !combat.CanBlock(asBoard(e), bystander, attacker) {
		t.Fatal("precondition: marked and unmarked blocker pairs must be legal")
	}
	effects.Resolve(e, &effects.Ctx{Source: web, Controller: 0, SVars: face.SVars, Remembered: []state.Target{{Obj: attacker}}}, sa)
	board := asBoard(e)
	if !combat.MustBlockPairRequired(board, blocker, attacker) || combat.MustBlockPairRequired(board, blocker, otherAttacker) ||
		combat.MustBlockCandidates(board, 0)[bystander] {
		t.Fatal("defined-set duty did not scope marked blocker to triggered attacker")
	}
	d := askBlockersFresh(t, e)
	if d == nil || d.Kind != decision.KBlockers {
		t.Fatalf("precondition: missing blockers decision: %#v", d)
	}
	pair, unmarked := findBlockOption(d, blocker, attacker), findBlockOption(d, bystander, attacker)
	other := findBlockOption(d, blocker, otherAttacker)
	if pair == nil || unmarked == nil || other == nil || !pair.Required || !pair.BlockMust ||
		unmarked.Required || unmarked.BlockMust || other.BlockMust {
		t.Fatalf("Magnetic Web options incorrect: %+v", d.Options)
	}
	in := decision.Intent{Player: d.Player, Seq: d.Seq, Choices: []int{pair.Index}}
	if err := d.Validate(in); err != nil {
		t.Fatalf("decision rejected marked pair: %v", err)
	}
	if err := e.validateBlockers(d, in); err != nil {
		t.Fatalf("engine rejected marked pair: %v", err)
	}
	team := d.BlockRequiredTeam()
	if len(team) != 1 || team[0] != pair.Index {
		t.Fatalf("decision required team = %v, want marked pair %d", team, pair.Index)
	}
	for _, choices := range [][]int{{}, {unmarked.Index}} {
		in.Choices = choices
		if err := e.validateBlockers(d, in); err == nil {
			t.Fatalf("engine accepted declaration omitting required blocker: %v", choices)
		}
	}
	// Ordinary MustBlock permits the same creature to satisfy its duty by
	// blocking a different attacker. That alternative is not itself a named
	// required pair (BlockMust remains false on the other option).
	in.Choices = []int{other.Index}
	if err := d.Validate(in); err != nil {
		t.Fatalf("decision rejected ordinary alternative: %v", err)
	}
	if err := e.validateBlockers(d, in); err != nil {
		t.Fatalf("engine rejected ordinary alternative: %v", err)
	}
}
