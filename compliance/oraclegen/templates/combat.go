// Level-B combat templates (spec
// docs/superpowers/specs/2026-10-05-compliance-level-b.md section 2.4).
package templates

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// CombatAttack builds attack and block observations for combat requirements.
var CombatAttack = Template{ID: "combat", Version: 1}

const combatBlocker = "Grizzly Bears"

func combatSubs(sub string) bool {
	return sub == "combat.attack" || sub == "combat.block"
}

func combatRequirement(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement) (oraclegen.Item, *oraclegen.Skip) {
	skip := func(reason string) (oraclegen.Item, *oraclegen.Skip) {
		return oraclegen.Item{}, &oraclegen.Skip{Card: name, Reason: "combat " + reason}
	}
	bear, ok := reg.Lookup(combatBlocker)
	if !ok || len(bear.Faces) == 0 || !bear.Faces[0].IsCreature() {
		return skip("fixture blocker not in corpus")
	}
	var sc oraclegen.Scenario
	switch req.Sub {
	case "combat.attack":
		sc = combatScenario(f, name, req, []oraclegen.Step{
			{Op: "attack", Seat: 0, Defender: "p1", Attackers: []string{"p0:" + name}},
			{Op: "block", Seat: 1, Blocks: [][2]string{{"p1:" + combatBlocker, "p0:" + name}}},
			{Op: "pass_to", Seat: 0, Step: "main2"},
		})
		// Some attackers (notably flying creatures) cannot be blocked by the
		// fixture. Gorge's own decision is authoritative: keep the block only
		// when the runner accepts it, otherwise retain the attack observation.
		if _, ok := oraclegen.PlaysThrough(reg, sc); !ok {
			sc.Steps = []oraclegen.Step{sc.Steps[0], sc.Steps[2]}
		}
	case "combat.block":
		if !f.IsCreature() {
			return skip("block not offered")
		}
		sc = combatScenario(f, name, req, []oraclegen.Step{
			{Op: "attack", Seat: 1, Defender: "p0", Attackers: []string{"p1:" + combatBlocker}},
			{Op: "block", Seat: 0, Blocks: [][2]string{{"p0:" + name, "p1:" + combatBlocker}}},
			{Op: "pass_to", Seat: 0, Step: "main2", Active: "p1"},
		})
	}
	if sc.Setup == nil {
		return skip("unsupported requirement " + req.Sub)
	}
	n, res, ok := oraclegen.Settle(reg, sc)
	if !ok || len(res.Fails) != 0 {
		if req.Sub == "combat.block" {
			return skip("block not offered")
		}
		return skip("scenario does not replay")
	}
	for i := 0; i < n; i++ {
		sc.Steps = append(sc.Steps, oraclegen.Step{Op: "resolve"})
	}
	sc, castSteps := oraclegen.ChooseTargets(sc, res.Decisions)
	res, ok = oraclegen.PlaysThrough(reg, sc)
	if !ok || len(res.Fails) != 0 {
		return skip("scenario does not replay")
	}
	if yes, changed := oraclegen.MayYes(sc, res.Decisions); changed {
		if res2, yesOK := oraclegen.PlaysThrough(reg, yes); yesOK && len(res2.Fails) == 0 {
			sc, res = yes, res2
		}
	}
	it := oraclegen.NewLevelBItem(name, req.Key, CombatAttack.Version, []string{"506", "508", "509", "510"}, sc)
	it.XAnswers = oraclegen.XAnswersForScenario(res, sc, oraclegen.ModeNumbers(f), castSteps)
	return it, nil
}

func combatScenario(f *cards.Face, name string, req levelb.Requirement, steps []oraclegen.Step) oraclegen.Scenario {
	p0 := oraclegen.Seat{Battlefield: []string{name}}
	setupBackFace(&p0, name, req)
	sc := oraclegen.Scenario{
		Setup: map[string]oraclegen.Seat{
			"p0": p0,
			"p1": {Battlefield: []string{combatBlocker}},
		},
		SetupAnswers: oraclegen.OpeningHandAnswers(f),
		Steps:        append([]oraclegen.Step(nil), steps...),
	}
	oraclegen.Baseline(sc.Setup, f)
	return sc
}
