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
		"trigger.spell-cast", "trigger.spell-cast-self", "trigger.becomes-target", "trigger.life-gained", "trigger.drawn", "trigger.phase",
		"trigger.dies-other", "trigger.scry", "trigger.surveil", "trigger.noncombat-damage", "trigger.combat-damage-all",
		"trigger.loyalty-activated", "trigger.discarded", "trigger.attacks-one-target":
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
		"main1@p0",
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
	fxs := triggerFixtures(reg, f, &f.Triggers[idx])
	fired := false
	for _, c := range causes {
		it, ok, didFire := triggerWith(reg, f, name, req, c, fxs)
		fired = fired || didFire
		if ok {
			return it, nil
		}
	}
	// A creature that setup would leave 0/0 dies as a state-based action
	// before it can trigger: retry with what keeps it alive.
	for _, fix := range []func(*cards.Face, *triggerCause) bool{castXCreature, islandsForStarPT} {
		for _, c := range causes {
			if c.selfInHand || !fix(f, &c) {
				continue
			}
			it, ok, didFire := triggerWith(reg, f, name, req, c, fxs)
			fired = fired || didFire
			if ok {
				return it, nil
			}
		}
	}
	if !fired {
		return skip("did not fire")
	}
	return skip("no fixture gorge can play")
}

func triggerScenario(f *cards.Face, name string, c triggerCause, req levelb.Requirement, steps []oraclegen.Step, fx *oraclegen.Fixture) oraclegen.Scenario {
	p0, p1 := mergedFixtureSeats(fx)
	p0.Battlefield = appendFixtureUnique(p0.Battlefield, name)
	p0.Hand = append(p0.Hand, c.hand...)
	if c.selfInHand || c.castSelfX {
		p0.Battlefield = removeString(p0.Battlefield, name)
	} else {
		setupBackFace(&p0, name, req)
	}
	for _, b := range c.battlefield {
		p0.Battlefield = appendFixtureUnique(p0.Battlefield, b)
	}
	sc := oraclegen.Scenario{
		Setup:        map[string]oraclegen.Seat{"p0": p0, "p1": p1},
		SetupAnswers: oraclegen.OpeningHandAnswers(f),
		Steps:        triggerSteps(f, name, c, steps, fx),
	}
	if c.castSelfX {
		sc.Setup["p0"] = oraclegen.WithHand(sc.Setup["p0"], name)
	}
	oraclegen.Baseline(sc.Setup, f)
	return sc
}

// triggerWith tries one cause. fired reports that gorge put the card's
// trigger on the stack, so a caller can tell "did not fire" from a replay
// failure.
func triggerWith(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement, c triggerCause, fxs []oraclegen.Fixture) (it oraclegen.Item, ok, fired bool) {
	for i := range fxs {
		it, ok, didFire := triggerWithFixture(reg, f, name, req, c, &fxs[i])
		fired = fired || didFire
		if ok {
			return it, true, fired
		}
	}
	return it, false, fired
}

// triggerWithFixture tries one cause against one target fixture.
func triggerWithFixture(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement, c triggerCause, fx *oraclegen.Fixture) (it oraclegen.Item, ok, fired bool) {
	probe := c.probeSteps
	if probe == nil {
		probe = c.steps
	}
	// A cast cause leaves only the spell on the stack, and resolve clears
	// triggers as well, so the probe retries with both players passing once:
	// the spell resolves and the trigger it caused is on the stack.
	passes := []oraclegen.Step{{Op: "pass", Seat: 0}, {Op: "pass", Seat: 1}}
	for _, steps := range [][]oraclegen.Step{probe, append(append([]oraclegen.Step(nil), probe...), passes...)} {
		if _, res, ok := oraclegen.Settle(reg, triggerScenario(f, name, c, req, steps, fx)); ok && abilityOnStack(res.Snapshots, name) {
			fired = true
			break
		}
	}
	if !fired {
		return it, false, false
	}
	sc := triggerScenario(f, name, c, req, c.steps, fx)
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
	if c.xability != nil {
		it.XAbility = make([]string, len(sc.Steps))
		copy(it.XAbility[len(triggerSteps(f, name, c, nil, fx)):], c.xability)
	}
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
