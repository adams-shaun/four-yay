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

func mustBlockCorpusBody(t *testing.T, e *Engine, source state.ObjID, sVar string) (*cards.SA, *cards.Face) {
	t.Helper()
	face := e.G.Obj(source).Face()
	sa := cards.ResolveSVar(face.SVars, sVar)
	if sa == nil || sa.API != "MustBlock" || !cards.MustBlockNamedTargetShape(sa) {
		t.Fatalf("precondition: corpus %s SVar %q is not a supported MustBlock body: %#v", face.Name, sVar, sa)
	}
	return sa, face
}

// Fighter Class's TargetMin$ 0 means an unchosen target is an empty set,
// never Defined's ordinary no-target fallback to the source creature.
func TestMustBlockCorpusFighterClassOptionalTarget(t *testing.T) {
	e := threeSeatEngine(t)
	class := onBoardCard(t, e, 0, mshCorpusCard(t, "Fighter Class"))
	attacker := onBoard(t, e, 1, "Name:Attacker\nTypes:Creature\nPT:2/2\nOracle:x\n")
	blocker := onBoard(t, e, 0, "Name:Blocker\nTypes:Creature\nPT:2/2\nOracle:x\n")
	e.G.Obj(attacker).IsAttacking, e.G.Obj(attacker).Attacking = true, 0
	sa, face := mustBlockCorpusBody(t, e, class, "TrigMustBlock")
	if sa.ParamStr(cards.PKTargetMin) != "0" {
		t.Fatalf("precondition: optional target minimum is %q", sa.ParamStr(cards.PKTargetMin))
	}
	effects.Resolve(e, &effects.Ctx{Source: class, Controller: 0, SVars: face.SVars, Targets: []state.Target{}, PickedTargets: []state.Target{}, Remembered: []state.Target{{Obj: attacker}}, TargetsOffered: true}, sa)
	if combat.MustBlockCandidates(asBoard(e), 0)[class] || combat.MustBlockPairRequired(asBoard(e), class, attacker) || combat.MustBlockCandidates(asBoard(e), 0)[blocker] {
		t.Fatal("optional unchosen target created a source or blanket blocking duty")
	}
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && ev.Text == "MustBlock selector shape unimplemented" {
			t.Fatal("precondition: optional supported handler degraded as unimplemented")
		}
	}
}

// Magnetic Web's Defined$ Valid selector is a set, not the selected target:
// only creatures carrying the printed magnet counter become blockers.
func TestMustBlockCorpusMagneticWebDefinedSet(t *testing.T) {
	e := threeSeatEngine(t)
	web := onBoardCard(t, e, 0, mshCorpusCard(t, "Magnetic Web"))
	attacker := onBoard(t, e, 1, "Name:Magnet attacker\nTypes:Creature\nPT:2/2\nOracle:x\n")
	blocker := onBoard(t, e, 0, "Name:Magnet blocker\nTypes:Creature\nPT:2/2\nOracle:x\n")
	bystander := onBoard(t, e, 0, "Name:Unmarked blocker\nTypes:Creature\nPT:2/2\nOracle:x\n")
	for _, id := range []state.ObjID{attacker} {
		e.G.Obj(id).IsAttacking, e.G.Obj(id).Attacking = true, 0
	}
	e.emit(events.Event{Kind: events.CounterChange, Obj: blocker, Counter: "MAGNET", Amount: 1})
	if e.G.Obj(blocker).Counter("MAGNET") == 0 || e.G.Obj(bystander).Counter("MAGNET") != 0 {
		t.Fatal("precondition: Magnetic Web filter values do not differ")
	}
	sa, face := mustBlockCorpusBody(t, e, web, "TrigLure")
	effects.Resolve(e, &effects.Ctx{Source: web, Controller: 0, SVars: face.SVars, Remembered: []state.Target{{Obj: attacker}}}, sa)
	b := asBoard(e)
	if !combat.MustBlockPairRequired(b, blocker, attacker) || combat.MustBlockCandidates(b, 0)[bystander] {
		t.Fatal("Magnetic Web did not require exactly the marked creature")
	}
}

// Blaze of Glory binds ParentTarget as the blocker and every current attacker
// as a separate pair duty. The pair oracle feeds declaration validation too.
func TestMustBlockCorpusBlazeOfGloryAllDefinedAttackers(t *testing.T) {
	e := threeSeatEngine(t)
	blaze := onBoardCard(t, e, 1, mshCorpusCard(t, "Blaze of Glory"))
	blocker := onBoard(t, e, 0, "Name:Chosen blocker\nTypes:Creature\nPT:2/2\nOracle:x\n")
	bystander := onBoard(t, e, 0, "Name:Other blocker\nTypes:Creature\nPT:2/2\nOracle:x\n")
	a1 := onBoard(t, e, 1, "Name:Attacker one\nTypes:Creature\nPT:2/2\nOracle:x\n")
	a2 := onBoard(t, e, 1, "Name:Attacker two\nTypes:Creature\nPT:2/2\nOracle:x\n")
	attackSeat0(t, e, a1, a2)
	if e.G.Obj(blocker).Zone != state.ZBattlefield || e.G.Obj(bystander).Zone != state.ZBattlefield || a1 == a2 {
		t.Fatal("precondition: invalid Blaze battlefield/combat setup")
	}
	_, face := mustBlockCorpusBody(t, e, blaze, "GoingDownInStyle")
	sa := cards.ResolveSVar(face.SVars, "GoingDownInStyle")
	effects.Resolve(e, &effects.Ctx{Source: blaze, Controller: 1, SVars: face.SVars, Targets: []state.Target{{Obj: blocker}}, TargetsOffered: true}, sa)
	b := asBoard(e)
	for _, attacker := range []state.ObjID{a1, a2} {
		if !combat.MustBlockPairRequired(b, blocker, attacker) {
			t.Fatalf("chosen blocker is not required to block attacker %d", attacker)
		}
	}
	if combat.MustBlockCandidates(b, 1)[bystander] {
		t.Fatal("unselected blocker acquired a duty")
	}
	d := askBlockersFresh(t, e)
	if d == nil || d.Kind != decision.KBlockers {
		t.Fatalf("precondition: missing blockers decision: %#v", d)
	}
	var indexes []int
	for i, o := range d.Options {
		if o.Obj == blocker && (o.Attacker == a1 || o.Attacker == a2) {
			if !o.Required || !o.BlockMust {
				t.Fatalf("required pair lacks flags: %+v", o)
			}
			indexes = append(indexes, i)
		}
	}
	if len(indexes) != 2 {
		t.Fatalf("precondition: expected two offered required pairs, got %d", len(indexes))
	}
	if d.GroupCapFor(d.Options[indexes[0]].Group) != 2 {
		t.Fatalf("BlockAllDefined group cap = %d, want 2", d.GroupCapFor(d.Options[indexes[0]].Group))
	}
	team := d.BlockRequiredTeam()
	if len(team) != 2 || team[0] == team[1] {
		t.Fatalf("decision required team = %v, want both BlockAllDefined pairs", team)
	}
	both := decision.Intent{Player: 0, Seq: d.Seq, Choices: indexes}
	if err := d.Validate(both); err != nil {
		t.Fatalf("decision rejected blocking both defined attackers: %v", err)
	}
	if err := e.validateBlockers(d, both); err != nil {
		t.Fatalf("validator rejected blocking both defined attackers: %v", err)
	}
	if err := e.validateBlockers(d, decision.Intent{Player: 0, Choices: []int{indexes[0]}}); err == nil {
		t.Fatal("validator accepted declaration omitting one BlockAllDefined duty")
	}
	if err := e.validateBlockers(d, decision.Intent{Player: 0}); err == nil {
		t.Fatal("validator accepted declaration omitting all-defined duties")
	}
}
