// Level-B trigger template (spec
// docs/superpowers/specs/2026-10-05-compliance-level-b.md section 2.2; ticket
// L7). The card sits on p0's battlefield with the recipe's probe; the steps
// are the recipe's cause and then resolve. A candidate is served only when
// gorge shows the card's trigger on the stack (or, for combat damage, in the
// combat-damage step), so an item never asserts a trigger that did not fire.
// Settling and the target and answer rewrite follow castWith.
package templates

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/rules"
)

// TriggerFires causes a trigger on turn 1 and resolves it. Its own version:
// bumping it stales only this family's level-B rows.
var TriggerFires = Template{ID: "trigger", Version: 1}

// triggerSubs are the level-B sub-families this template serves; every
// trigger.gap:* still skips.
func triggerSubs(sub string) bool {
	switch sub {
	case "trigger.etb-other", "trigger.dies", "trigger.attacks", "trigger.combat-damage",
		"trigger.spell-cast", "trigger.becomes-target", "trigger.life-gained", "trigger.drawn", "trigger.phase":
		return true
	}
	return false
}

// PassToSteps returns the exact pass_to checkpoints emitted by current
// level-B templates. Entries are step names; when active is required, the
// entry is "step@pN". Player-wide upkeep/draw recipes stop at p1 on turn 2,
// while You-only recipes wait for p0 on turn 3.
func PassToSteps() []string {
	return []string{
		"begin-combat",
		"draw@p0",
		"draw@p1",
		"end",
		"end-combat",
		"main2",
		"upkeep@p0",
		"upkeep@p1",
	}
}

// triggerFires builds the scenario serving one trigger requirement.
func triggerFires(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement) (oraclegen.Item, *oraclegen.Skip) {
	skip := func(why string) (oraclegen.Item, *oraclegen.Skip) {
		return oraclegen.Item{}, &oraclegen.Skip{Card: name, Reason: "trigger " + why}
	}
	idx, err := strconv.Atoi(req.Slot)
	if err != nil || idx < 0 || idx >= len(f.Triggers) {
		return skip("index " + req.Slot)
	}
	if req.CoveredByA {
		return skip("covered by level A")
	}
	causes, why := triggerRecipe(reg, f, name, &f.Triggers[idx], req.Sub)
	if why != "" {
		return skip("no recipe: " + why)
	}
	fired := false
	for _, c := range causes {
		it, ok, didFire := triggerWith(reg, f, name, req, c)
		fired = fired || didFire
		if ok {
			return it, nil
		}
	}
	if !fired {
		return skip("did not fire")
	}
	return skip("no fixture gorge can play")
}

func triggerScenario(f *cards.Face, name string, c triggerCause, steps []oraclegen.Step) oraclegen.Scenario {
	p0 := oraclegen.Seat{Battlefield: []string{name}, Hand: append([]string(nil), c.hand...)}
	for _, b := range c.battlefield {
		p0.Battlefield = appendFixtureUnique(p0.Battlefield, b)
	}
	sc := oraclegen.Scenario{
		Setup:        map[string]oraclegen.Seat{"p0": p0, "p1": {}},
		SetupAnswers: oraclegen.OpeningHandAnswers(f),
		Steps:        append([]oraclegen.Step(nil), steps...),
	}
	oraclegen.Baseline(sc.Setup, f)
	return sc
}

// triggerWith tries one cause. fired reports that gorge put the card's
// trigger on the stack, so a caller can tell "did not fire" from a replay
// failure.
func triggerWith(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement, c triggerCause) (it oraclegen.Item, ok, fired bool) {
	probe := c.probeSteps
	if probe == nil {
		probe = c.steps
	}
	// A cast cause leaves only the spell on the stack, and resolve clears
	// triggers as well, so the probe retries with both players passing once:
	// the spell resolves and the trigger it caused is on the stack.
	passes := []oraclegen.Step{{Op: "pass", Seat: 0}, {Op: "pass", Seat: 1}}
	for _, steps := range [][]oraclegen.Step{probe, append(append([]oraclegen.Step(nil), probe...), passes...)} {
		if _, res, ok := oraclegen.Settle(reg, triggerScenario(f, name, c, steps)); ok && abilityOnStack(res.Snapshots, name) {
			fired = true
			break
		}
	}
	if !fired {
		return it, false, false
	}
	sc := triggerScenario(f, name, c, c.steps)
	n, res, ok := oraclegen.Settle(reg, sc)
	if !ok {
		return it, false, true
	}
	for i := 0; i < n; i++ {
		sc.Steps = append(sc.Steps, oraclegen.Step{Op: "resolve"})
	}
	sc, castSteps := oraclegen.ChooseTargets(sc, res.Decisions)
	res, ok = oraclegen.PlaysThrough(reg, sc)
	if !ok {
		return it, false, true
	}
	if yes, changed := oraclegen.MayYes(sc, res.Decisions); changed {
		if res2, ok2 := oraclegen.PlaysThrough(reg, yes); ok2 {
			sc, res = yes, res2
		}
	}
	it = oraclegen.NewLevelBItem(name, req.Key, TriggerFires.Version, []string{"603.2"}, sc)
	it.XAnswers = oraclegen.XAnswersForScenario(res, sc, oraclegen.ModeNumbers(f), castSteps)
	return it, true, true
}

// abilityOnStack reports whether any snapshot shows an ability stack entry
// whose source is the named card.
func abilityOnStack(snaps []rules.OracleSnapshot, name string) bool {
	want := strings.ToLower(name)
	for _, s := range snaps {
		for _, e := range s.Stack {
			if e.Kind == "ability" && strings.Contains(strings.ToLower(e.Source), want) {
				return true
			}
		}
	}
	return false
}
