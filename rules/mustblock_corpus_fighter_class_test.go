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

func TestMustBlockCorpusFighterClassSelectedTarget(t *testing.T) {
	for _, selected := range []bool{false, true} {
		name := "zero targets"
		if selected {
			name = "selected target"
		}
		t.Run(name, func(t *testing.T) {
			e := threeSeatEngine(t)
			card := mshCorpusCard(t, "Fighter Class")
			source := onBoardCard(t, e, 0, card)
			attacker := onBoard(t, e, 0, "Name:Class Attacker\nTypes:Creature Warrior\nPT:3/3\nOracle:x\n")
			blocker := onBoard(t, e, 1, "Name:Chosen Blocker\nTypes:Creature Bear\nPT:3/3\nOracle:x\n")
			bystander := onBoard(t, e, 1, "Name:Unrelated Blocker\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
			otherAttacker := onBoard(t, e, 0, "Name:Other Attacker\nTypes:Creature Warrior\nPT:2/2\nOracle:x\n")
			for label, id := range map[string]state.ObjID{"source": source, "attacker": attacker, "blocker": blocker, "bystander": bystander, "other attacker": otherAttacker} {
				if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
					t.Fatalf("precondition: %s %d is not on battlefield", label, id)
				}
			}
			for _, id := range []state.ObjID{attacker, otherAttacker} {
				o := e.G.Obj(id)
				o.IsAttacking, o.Attacking = true, 1
			}
			if attacker == otherAttacker || blocker == bystander || !combat.CanBlock(asBoard(e), blocker, attacker) {
				t.Fatal("precondition: distinct attackers/blockers and legal chosen block required")
			}

			face := e.G.Obj(source).Face()
			sa := cards.ResolveSVar(face.SVars, "TrigMustBlock")
			if sa == nil || sa.API != "MustBlock" || sa.Params["DefinedAttacker"] != "TriggeredAttackerLKICopy" || sa.Params["ValidTgts"] != "Creature" || sa.Params["TargetMin"] != "0" || sa.Params["TargetMax"] != "1" || sa.Params["BlockAllDefined"] != "True" || sa.Params["Duration"] != "UntilEndOfCombat" {
				t.Fatalf("precondition: corpus TrigMustBlock shape changed: %#v", sa)
			}
			var targets []state.Target
			if selected {
				targets = []state.Target{{Obj: blocker}}
			}
			e.G.Active = 0
			e.G.Step = state.StepDeclareBlockers
			effects.Resolve(e, &effects.Ctx{
				Source: source, Controller: 0, SVars: face.SVars,
				Remembered: []state.Target{{Obj: attacker}}, Targets: targets, TargetsOffered: true,
			}, sa)
			for _, ev := range e.L.Events {
				if ev.Kind == events.Note && ev.Text == "MustBlock selector shape unimplemented" {
					t.Fatalf("Fighter Class MustBlock handler rejected corpus shape: %+v", ev)
				}
			}
			candidates := combat.MustBlockCandidates(asBoard(e), 1)
			if selected {
				if !candidates[blocker] || candidates[bystander] || !combat.MustBlockPairRequired(asBoard(e), blocker, attacker) {
					t.Fatalf("selected blocker duty missing or overbroad: candidates=%v", candidates)
				}
				if combat.MustBlockPairRequired(asBoard(e), blocker, otherAttacker) {
					t.Fatal("selected blocker is required to block an unrelated attacker")
				}
			} else if len(candidates) != 0 {
				t.Fatalf("zero-target trigger created blocking duty: %v", candidates)
			}

			e.askBlockers()
			d := e.Pending()
			if d == nil || d.Kind != decision.KBlockers {
				t.Fatalf("declare-blockers decision missing: %+v", d)
			}
			var requiredIndex = -1
			for i, opt := range d.Options {
				if opt.Obj == blocker && opt.Attacker == attacker {
					if selected && opt.Required {
						requiredIndex = i
					}
					if !selected && opt.Required {
						t.Fatalf("zero-target branch marks block required: %+v", opt)
					}
				}
				if selected && opt.Obj == bystander && opt.Required {
					t.Fatalf("unrelated blocker option became required: %+v", opt)
				}
			}
			if selected {
				if requiredIndex < 0 {
					t.Fatal("selected blocker/trigger attacker pair is not Required in declaration options")
				}
				in := decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{requiredIndex}}
				if err := d.Validate(in); err != nil {
					t.Fatalf("required declare-blockers option rejected by decision validator: %v", err)
				}
				if err := e.validateBlockers(d, in); err != nil {
					t.Fatalf("required declare-blockers option rejected by declaration validator: %v", err)
				}
			}
		})
	}
}
