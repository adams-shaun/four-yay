// Level-B trigger template (spec
// docs/superpowers/specs/2026-10-05-compliance-level-b.md section 2.2; ticket
// L7). The card sits on p0's battlefield with the recipe's probe; the steps
// are the recipe's cause and then resolve. A candidate is served only when
// gorge shows the card's trigger on the stack (or, for combat damage, in the
// combat-damage step), so an item never asserts a trigger that did not fire.
// Settling and the target and answer rewrite follow castWith.
package templates

import (
	"sort"
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
	case "trigger.etb-other", "trigger.etb-land", "trigger.dies", "trigger.leaves-graveyard", "trigger.ltb-other", ltbSelfSub, "trigger.attacks", "trigger.combat-damage",
		"trigger.spell-cast", "trigger.spell-cast-self", "trigger.becomes-target", "trigger.life-gained", "trigger.drawn", "trigger.phase", levelb.PhaseOtherSub,
		"trigger.dies-other", "trigger.zone-change-residue", "trigger.scry", "trigger.surveil", "trigger.noncombat-damage", "trigger.combat-damage-all",
		"trigger.loyalty-activated", "trigger.discarded", "trigger.attacks-one-target", classLevelGainedSub,
		"trigger.spell-cast-opponent", "trigger.spell-cast-self-cast", "trigger.spell-cast-opponent-turn", "trigger.commit-crime", "trigger.ability-activated",
		levelb.UnlockDoorSub, levelb.FullyUnlockSub, stateSelfCountersSub, levelb.CounterAddedSub,
		levelb.TurnedFaceUpSub, levelb.TurnedFaceUpOtherSub, levelb.SacrificeSub, levelb.DamageSub:
		return true
	}
	return tapCombatSub(sub)
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
		// The opponent-turn cast cause passes to p1's main phase before p0
		// casts there.
		"main1@p1",
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
	causes, why, room := roomTriggerCauses(reg, name, req)
	if !room {
		causes, why = triggerRecipe(reg, f, name, &f.Triggers[idx], req.Sub)
		causes = roomSecondDoorCauses(reg, name, req, causes)
	}
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
	// A sibling trigger that fires on the cause's own cast (a repartee)
	// resolves ahead of the spell and spends the standard variants' two
	// passes; two more pass pairs let the death reach the stack. Only a card
	// with a second trigger can need it.
	if !fired && len(f.Triggers) > 1 {
		for _, c := range causes {
			it, ok, didFire := triggerWithLong(reg, f, name, req, c, fxs)
			fired = fired || didFire
			if ok {
				return it, nil
			}
		}
	}
	if !fired {
		if attackTriggerSub(req.Sub) {
			for _, cause := range causes {
				for _, step := range append(append([]oraclegen.Step(nil), cause.prelude...), cause.steps...) {
					if step.Op == "activate" {
						return skip("attack activation did not fire")
					}
				}
			}
		}
		if req.Sub == "trigger.spell-cast" || req.Sub == "trigger.spell-cast-opponent" || req.Sub == "trigger.spell-cast-opponent-turn" {
			if reason := spellCastNarrowSkip(&f.Triggers[idx]); reason != "" {
				return skip(reason)
			}
		}
		if req.Sub == "trigger.dies-other" {
			if reason := diesVictimSkip(&f.Triggers[idx]); reason != "" {
				return skip(reason)
			}
		}
		if band := classBandLevel(&f.Triggers[idx]); band >= 2 {
			// The cause prepends the level-up prelude; only when that prelude
			// could not be built is the class level itself the reason the
			// trigger did not fire. Once the prelude ran, a non-firing trigger
			// is its own cause's failure, reported below.
			_, _, preludeOK := classLevelPrelude(f, name, band)
			if !preludeOK {
				return skip("condition: class level")
			}
		}
		// A graveyard-source trigger keeps its own, narrower reason below.
		if req.Sub == "trigger.phase" || req.Sub == levelb.PhaseOtherSub || (conditionTriggerSub(req.Sub) && !triggerFromGraveyard(f, req)) {
			if reason := triggerConditionSkip(&f.Triggers[idx], f.SVars); reason != "" {
				return skip(reason)
			}
		}
		if triggerFromGraveyard(f, req) {
			// The card sat in the graveyard, where the trigger functions,
			// and its own condition (a threshold, an event count) still
			// held it back: a named reason, not the bare "did not fire".
			return skip("did not fire from the graveyard")
		}
		return skip("did not fire")
	}
	return skip("no fixture gorge can play")
}

func triggerScenario(f *cards.Face, name string, c triggerCause, req levelb.Requirement, steps []oraclegen.Step, fx *oraclegen.Fixture) oraclegen.Scenario {
	p0, p1 := mergedFixtureSeats(fx)
	grave := triggerFromGraveyard(f, req)
	if grave {
		p0.Graveyard = appendFixtureUnique(p0.Graveyard, name)
	} else {
		p0.Battlefield = appendFixtureUnique(p0.Battlefield, name)
	}
	p0.Hand = append(p0.Hand, c.hand...)
	p0.Exile = appendFixtureCounts(p0.Exile, c.exile)
	if filler := etbDiscardFiller(f); filler != "" && !grave && !c.selfInHand && !c.castSelfX {
		p0.Hand = append([]string{filler}, p0.Hand...)
	}
	if c.selfInHand || c.castSelfX {
		p0.Battlefield = removeString(p0.Battlefield, name)
		p0.Graveyard = removeString(p0.Graveyard, name)
	} else if !grave {
		setupBackFace(&p0, name, req)
	}
	p0.Battlefield = appendFixtureCounts(p0.Battlefield, c.battlefield)
	p0.Graveyard = appendFixtureCounts(p0.Graveyard, c.graveyard)
	p1.Hand = appendFixtureCounts(p1.Hand, c.opponentHand)
	p1.Battlefield = appendFixtureCounts(p1.Battlefield, c.opponentBattlefield)
	oppCounterCards := make([]string, 0, len(c.opponentCounters))
	for card := range c.opponentCounters {
		oppCounterCards = append(oppCounterCards, card)
	}
	sort.Strings(oppCounterCards)
	for _, card := range oppCounterCards {
		kinds := c.opponentCounters[card]
		counterKinds := make([]string, 0, len(kinds))
		for kind := range kinds {
			counterKinds = append(counterKinds, kind)
		}
		sort.Strings(counterKinds)
		for _, kind := range counterKinds {
			p1 = oraclegen.WithCounters(p1, card, kind, int32(kinds[kind]))
		}
	}
	for _, card := range c.tapped {
		if card == "__SOURCE__" {
			card = name
		}
		p0.Battlefield = appendFixtureUnique(p0.Battlefield, card)
		p0.Tapped = appendFixtureUnique(p0.Tapped, card)
	}
	counterCards := make([]string, 0, len(c.counters))
	for card := range c.counters {
		counterCards = append(counterCards, card)
	}
	sort.Strings(counterCards)
	for _, card := range counterCards {
		kinds := c.counters[card]
		if card == "__SOURCE__" {
			card = name
		}
		counterKinds := make([]string, 0, len(kinds))
		for kind := range kinds {
			counterKinds = append(counterKinds, kind)
		}
		sort.Strings(counterKinds)
		for _, kind := range counterKinds {
			count := kinds[kind]
			p0 = oraclegen.WithCounters(p0, card, kind, int32(count))
		}
	}
	scenarioSteps := triggerSteps(f, name, c, steps, fx)
	if len(c.prelude) > 0 {
		scenarioSteps = append(append([]oraclegen.Step(nil), c.prelude...), scenarioSteps...)
	}
	sc := oraclegen.Scenario{
		Setup:        map[string]oraclegen.Seat{"p0": p0, "p1": p1},
		SetupAnswers: oraclegen.OpeningHandAnswers(f),
		Steps:        scenarioSteps,
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

// triggerWithLong retries one cause with two more pass pairs: a sibling
// trigger that fires on the cause's own cast (Scolding Administrator's
// Repartee) resolves ahead of the spell, so the death the row trigger waits
// for reaches the stack after the standard variants' two passes have been
// spent on the sibling's resolution.
func triggerWithLong(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement, c triggerCause, fxs []oraclegen.Fixture) (it oraclegen.Item, ok, fired bool) {
	probe := c.probeSteps
	if probe == nil {
		probe = c.steps
	}
	longPasses := []oraclegen.Step{{Op: "pass", Seat: 0}, {Op: "pass", Seat: 1}, {Op: "pass", Seat: 0}, {Op: "pass", Seat: 1}}
	settled := append(append([]oraclegen.Step(nil), probe...), longPasses...)
	checkpoint := append(append([]oraclegen.Step(nil), settled...), oraclegen.Step{Op: "pass_to", Decision: "priority"})
	for i := range fxs {
		for _, steps := range [][]oraclegen.Step{settled, checkpoint} {
			_, res, ok := oraclegen.Settle(reg, triggerScenario(f, name, c, req, steps, &fxs[i]))
			if ok && abilityOnStack(res.Snapshots, name, f.Name, stackSlot(req)) {
				served, ok := triggerServe(reg, f, name, req, c, &fxs[i])
				if ok {
					return served, true, true
				}
			}
		}
	}
	return it, false, false
}

// triggerWithFixture tries one cause against one target fixture.
func triggerWithFixture(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement, c triggerCause, fx *oraclegen.Fixture) (it oraclegen.Item, ok, fired bool) {
	probe := c.probeSteps
	if probe == nil {
		probe = c.steps
	}
	// A cast cause leaves only the spell on the stack. Passing resolves it,
	// but simultaneous sibling triggers wait behind trigger_order; pass_to
	// answers that ask and exposes the queued abilities without resolving them.
	passes := []oraclegen.Step{{Op: "pass", Seat: 0}, {Op: "pass", Seat: 1}}
	settled := append(append([]oraclegen.Step(nil), probe...), passes...)
	checkpoint := append(append([]oraclegen.Step(nil), settled...), oraclegen.Step{Op: "pass_to", Decision: "priority"})
	ordered := append(append([]oraclegen.Step(nil), probe...), oraclegen.Step{Op: "pass_to", Decision: "priority"})
	// A destroy spell whose target put a sibling trigger on the stack (a
	// Valiant "becomes the target" trigger) needs a second pass cycle before
	// the spell resolves and the dies trigger behind it surfaces. It is a
	// separate variant so every detection the shorter passes already made is
	// unchanged; the emitted item's bytes never move (the variant is detection
	// only).
	deep := append(append([]oraclegen.Step(nil), probe...), passes...)
	deep = append(deep, passes...)
	for _, steps := range [][]oraclegen.Step{probe, settled, checkpoint, ordered, deep} {
		if _, res, ok := oraclegen.Settle(reg, triggerScenario(f, name, c, req, steps, fx)); ok && abilityOnStack(res.Snapshots, name, f.Name, stackSlot(req)) {
			fired = true
			break
		}
	}
	if !fired {
		return it, false, false
	}
	it, ok = triggerServe(reg, f, name, req, c, fx)
	return it, ok, true
}

// triggerServe builds the item for a cause whose fire the probe confirmed:
// triggerServe builds the item for a cause whose fire the probe confirmed:
// the scenario is played with the resolves that empty the stack, the target
// and answer rewrite follows, and the item carries the observed decisions.
func triggerServe(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement, c triggerCause, fx *oraclegen.Fixture) (oraclegen.Item, bool) {
	var it oraclegen.Item
	sc := triggerScenario(f, name, c, req, c.steps, fx)
	n, res, ok := oraclegen.Settle(reg, sc)
	if !ok {
		return it, false
	}
	// Only a target hidden behind a sibling's trigger_order ask needs the extra
	// checkpoint; an item whose trigger is already on the stack keeps its steps.
	if req.Sub == "trigger.phase" && !abilityOnStack(res.Snapshots, name, f.Name, stackSlot(req)) {
		for _, d := range res.Decisions {
			if d.GorgeKind == "trigger_order" {
				sc.Steps = append(sc.Steps, oraclegen.Step{Op: "pass_to", Decision: "priority"})
				break
			}
		}
	}
	for i := 0; i < n; i++ {
		sc.Steps = append(sc.Steps, oraclegen.Step{Op: "resolve"})
	}
	sc, castSteps := oraclegen.ChooseTargets(sc, res.Decisions)
	res, ok = oraclegen.PlaysThrough(reg, sc)
	if !ok {
		return it, false
	}
	if yes, changed := oraclegen.MayYes(sc, res.Decisions); changed {
		if res2, ok2 := oraclegen.PlaysThrough(reg, yes); ok2 {
			sc, res = yes, res2
		}
	}
	// A declined hidden library search — or, when the served effect chain
	// looks at an exile/library zone and asks "choose one of them" (Fireglass
	// Mentor), a declined two-option look pick: re-run taking the first
	// eligible card, so XMage's mandatory TargetCardInLibrary /
	// TargetCardInExile ask is answered with a card instead of the skip it
	// rejects.
	forced := false
	eff := servedTriggerEffect(f, req)
	var search oraclegen.Scenario
	var changed bool
	if hasExileLibraryLook(eff) {
		search, changed = oraclegen.SearchPicksFromLook(sc, res.Decisions)
	} else {
		search, changed = oraclegen.SearchPicks(sc, res.Decisions)
	}
	if changed {
		if res2, ok2 := oraclegen.PlaysThrough(reg, search); ok2 {
			sc, res = search, res2
			forced = true
		}
	}
	it = oraclegen.NewLevelBItem(name, req.Key, TriggerFires.Version, []string{"603.2"}, sc)
	it.XAnswers = oraclegen.XAnswersForScenario(res, sc, oraclegen.ModeNumbers(f), castSteps)
	if forced {
		it.XAnswers = oraclegen.RetargetForcedLookPicks(it.XAnswers, res.Decisions)
	}
	it.XAnswers = scriptPreludeSacrifice(it.XAnswers, c.prelude, len(sc.Steps))
	it.XAnswers = scriptPreludeActivationCost(it.XAnswers, c.prelude, c.preludeActivationCost, len(sc.Steps), res.Decisions)
	it.XAnswers = scriptCauseActivationCost(it.XAnswers, c, sc.Steps, res.Decisions)
	// A "sacrifice a permanent unless you discard a card" decline: XMage
	// poses the cost's own ask on the target queue (trigger_declines.go).
	it.XAnswers = scriptSacrificeUnlessDiscardDecline(it.XAnswers, eff, res.Decisions)
	// A declined one-card pick after the chain's hidden-zone look: XMage
	// poses it without a chooseUse (trigger_declines.go).
	it.XAnswers = scriptLookedPickDecline(it.XAnswers, eff, res.Decisions)
	// Ward is caused by targeting; decline its unless-pay mode so the probe
	// does not depend on the opponent's ability to pay the ward cost.
	for _, d := range res.Decisions {
		if req.Sub != "trigger.becomes-target" || len(c.opponentHand) == 0 || d.Resume != "unless_pay" || d.Step < 0 || d.Step >= len(it.XAnswers) {
			continue
		}
		for i := range it.XAnswers[d.Step] {
			if it.XAnswers[d.Step][i].Kind == "mode" {
				it.XAnswers[d.Step][i].Value = "[mode_skip]"
			}
		}
	}
	if c.xability != nil || len(c.preludeXAbility) > 0 {
		it.XAbility = make([]string, len(sc.Steps))
		copy(it.XAbility, c.preludeXAbility)
		if c.xability != nil {
			offset := len(c.prelude) + len(triggerSteps(f, name, c, nil, fx))
			copy(it.XAbility[offset:], c.xability)
		}
	}
	return it, true
}

// stackSlot is the Trigger index gorge stamps on the stack entry that the
// requirement's trigger puts there. A door's "when you unlock this door" is
// queued as a delayed-shape trigger (rules/rooms.go queueUnlockTriggers), which
// carries no slot, so its entry is told apart by its source alone.
func stackSlot(req levelb.Requirement) string {
	if req.Sub == levelb.UnlockDoorSub {
		return ""
	}
	return req.Slot
}

// triggerFromGraveyard reports whether the trigger indexed by req functions
// from its owner's graveyard (TriggerZones$ names Graveyard). Such a trigger
// only fires with its source in the graveyard, so the scenario places the
// card there instead of on the battlefield. A slot that does not parse names
// no trigger and is not graveyard-scoped.
func triggerFromGraveyard(f *cards.Face, req levelb.Requirement) bool {
	idx, err := strconv.Atoi(req.Slot)
	if err != nil || idx < 0 || idx >= len(f.Triggers) {
		return false
	}
	return strings.Contains(strings.ToLower(f.Triggers[idx].ParamStr(cards.PKTriggerZones)), "graveyard")
}

// abilityOnStack reports whether any snapshot shows an ability stack entry
// whose source is the named card and that the card's trigger at slot put
// there: a card with several triggers (Kolodin's Mount and Vehicle ETBs) is
// not served by a probe that fires only a different one.
func abilityOnStack(snaps []rules.OracleSnapshot, name, faceName, slot string) bool {
	wants := []string{strings.ToLower(name), strings.ToLower(faceName)}
	for _, s := range snaps {
		for _, e := range s.Stack {
			if e.Kind != "ability" || e.Trigger != slot {
				continue
			}
			source := strings.ToLower(e.Source)
			for _, want := range wants {
				if want != "" && strings.Contains(source, want) {
					return true
				}
			}
		}
	}
	return false
}

// scriptCauseActivationCost carries the activate step's cost choices (the
// creatures a Crew or Saddle cost taps) to XMage, which asks for them. The
// standalone activate template exports them through the same helper; a cause
// that activates before attacking gets no such ask from XAnswersForScenario.
func scriptCauseActivationCost(xa [][]oraclegen.XAnswer, c triggerCause, steps []oraclegen.Step, decisions []rules.OracleDecision) [][]oraclegen.XAnswer {
	if c.activateCost == "" {
		return xa
	}
	if len(xa) == 0 {
		xa = make([][]oraclegen.XAnswer, len(steps))
	}
	addActivationCostAnswers(xa, activateStepIndex(steps), c.activateCost, decisions)
	return xa
}
