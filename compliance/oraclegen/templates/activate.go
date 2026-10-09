// Level-B activate template (spec
// docs/superpowers/specs/2026-10-05-compliance-level-b.md section 2.1; ticket
// L5). It serves the activate.battlefield and activate.mana requirements --
// and the channel-style activate.hand and activate.graveyard ones -- with the
// card on p0's battlefield (or in p0's hand or graveyard), the ability's targets from the ordinary fixture
// cross product, one `activate` step naming the ability by IR index, and a
// resolve for a non-mana ability.
//
// v1 cost tokens: mana, T, Q, PayLife<n>, one-card Discard, Sac (self or
// filtered other permanent), Exile<1/CARDNAME>, and in the matching zone
// Discard / ExileFromHand / ExileFromGrave of the source itself (plus one
// other graveyard creature card), supported tapXType fixture
// shapes, source loyalty AddCounter/SubCounter, and literal source-counter
// removal (SubCounter/RemoveAnyCounter, seeded by setup). Also supported are
// activate_keyword_costs.go's Waterbend, XMin, PayLife<X>, Blight, Forage,
// Exert and non-loyalty AddCounter. Anything else is a cost gap. The XMage
// rule-text prefix rides the Item's XAbility slice (parallel to Steps), so
// the runner -- which decodes steps strictly -- never sees it.
package templates

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/rules"
)

// ActivateAbility activates one activated ability and resolves it. Its own
// version: bumping it stales only this family's level-B rows.
var ActivateAbility = Template{ID: "activate", Version: 1}

// activateSubs are the level-B sub-families this template serves.
func activateSubs(sub string) bool {
	return sub == "activate.battlefield" || sub == "activate.mana" ||
		sub == "activate.hand" || sub == "activate.graveyard"
}

// activationZone names the zone p0's card starts in for a served sub-family:
// "hand" and "graveyard" for the channel-style abilities, "battlefield" for
// every other one (activate.mana included).
func activationZone(sub string) string {
	switch sub {
	case "activate.hand":
		return "hand"
	case "activate.graveyard":
		return "graveyard"
	}
	return "battlefield"
}

// activateAbility builds the scenario serving one activate requirement.
func activateAbility(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement) (oraclegen.Item, *oraclegen.Skip) {
	idx, err := strconv.Atoi(req.Slot)
	if err != nil || idx < 0 || idx >= len(f.Abilities) {
		return oraclegen.Item{}, &oraclegen.Skip{Card: name, Reason: "activate ability index " + req.Slot}
	}
	sa := f.Abilities[idx]
	prefixes, why := oraclegen.XMageAbility(f)
	if why != "" {
		return oraclegen.Item{}, &oraclegen.Skip{Card: name, Reason: why}
	}
	prefix, ok := prefixes[idx]
	if !ok {
		return oraclegen.Item{}, &oraclegen.Skip{Card: name, Reason: "activate xmage text ambiguous"}
	}
	zone := activationZone(req.Sub)
	pool, gap := activationCostIn(sa.ParamStr(cards.PKCost), zone, name, saXMin(sa))
	if gap != "" {
		return oraclegen.Item{}, &oraclegen.Skip{Card: name, Reason: "activate cost gap: " + gap}
	}
	slots := oraclegen.AbilitySlotSpecs(f, sa)
	// TargetsWithSameCreatureType$ (Secret Tunnel's "two target creatures you
	// control that share a creature type"): the fixture pair must share a
	// creature type, which the generic creature stand-ins never do. Mark the
	// slots so candidatesFor serves a matched same-subtype pair.
	if strings.EqualFold(strings.TrimSpace(sa.ParamStr(cards.PKTargetsWithSameCreatureType)), "True") {
		slots = markSameTypePairSlots(slots)
	}
	stackTargets := stackTargetRefs(sa, f, name, slots)
	for _, sl := range slots {
		// "Target creature that attacked this turn" needs a combat prelude
		// (attack, then back to a main phase for a sorcery-speed ability)
		// this template does not script; name the shape rather than report
		// a generic fixture miss.
		if attackedThisTurn(sl.Filter) {
			return oraclegen.Item{}, &oraclegen.Skip{Card: name,
				Reason: "activate target gap: attackedThisTurn needs a combat prelude (" + sl.Filter + ")"}
		}
	}
	// The board, graveyard and turn-history setup the ability's activation
	// restriction names. gap names the restriction shape no setup reaches, so
	// a failure is reported as a known restriction rather than the generic
	// no-fixture reason.
	restriction, gap := activateRestriction(reg, f, name, sa)
	it, ok := activateWith(reg, f, name, req, idx, prefix, pool, sa.ParamStr(cards.PKCost), zone, slots, restriction, stackTargets, abilityStackPlanOf(reg, sa, slots))
	if !ok {
		if oraclegen.HasType(f, "Aura") {
			// An Aura's ability is offered only while it is attached; this
			// template has no attach prelude, so name that cause.
			return oraclegen.Item{}, &oraclegen.Skip{Card: name,
				Reason: fmt.Sprintf("activate no fixture gorge can activate (Aura needs an attach prelude; targets %v)", filterStrings(slots))}
		}
		if gap != "" {
			return oraclegen.Item{}, &oraclegen.Skip{Card: name, Reason: gap}
		}
		return oraclegen.Item{}, &oraclegen.Skip{Card: name,
			Reason: fmt.Sprintf("activate no fixture gorge can activate (targets %v)", filterStrings(slots))}
	}
	return it, nil
}

// markSameTypePairSlots marks every slot's filter with sameTypePairMarker for
// an ability whose TargetsWithSameCreatureType$ demands a matched pair; the
// marker rides before any '@zone' suffix so the zone extraction stays intact.
func markSameTypePairSlots(slots []oraclegen.Slot) []oraclegen.Slot {
	out := make([]oraclegen.Slot, len(slots))
	for i, s := range slots {
		if j := strings.IndexByte(s.Filter, '@'); j >= 0 {
			s.Filter = s.Filter[:j] + "+" + oraclegen.SameTypePairMarker + s.Filter[j:]
		} else {
			s.Filter = s.Filter + "+" + oraclegen.SameTypePairMarker
		}
		out[i] = s
	}
	return out
}

// stackTargetRefs names the ref the stack slot's precast spell aims at: an
// ability whose TargetValidTargeting$ gate reads the held spell's own targets
// (Fugitive Droid's "Counter target spell that targets an artifact or
// creature you control") is offered only when the precast targets one of our
// battlefield permanents. Some alternative in the gate must name a YouCtrl
// Artifact or Creature for the source to satisfy it (it is on the
// battlefield); any other shape fails closed (nil), keeping the untargeted
// precast the only fixture.
func stackTargetRefs(sa *cards.SA, f *cards.Face, name string, slots []oraclegen.Slot) []string {
	stacked := false
	for _, sl := range slots {
		if oraclegen.SlotIsStack(sl.Filter) {
			stacked = true
		}
	}
	gate := strings.TrimSpace(sa.ParamStr(cards.PKTargetValidTargeting))
	if !stacked || gate == "" {
		return nil
	}
	for _, alt := range strings.Split(gate, ",") {
		alt = strings.ToLower(strings.TrimSpace(alt))
		base := strings.SplitN(alt, ".", 2)[0]
		has := (base == "artifact" && oraclegen.HasType(f, "Artifact")) ||
			(base == "creature" && oraclegen.HasType(f, "Creature"))
		if has && strings.Contains(alt, "youctrl") {
			return []string{"p0:" + name}
		}
	}
	return nil
}

// activateWith tries every fixture for the ability's target plan and returns
// the named level-B item. A restriction names the extra setup the ability's
// offer gates need; the bare scenario is tried first, then the restricted
// one. A non-nil plan is the ability-stack target shape (the probe prelude,
// activate_ability_stack.go), served apart from the ordinary cross product.
func activateWith(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement, idx int, prefix, mana, cost, zone string, slots []oraclegen.Slot, restrictions []conditionPrelude, stackTargets []string, plan *abilityStackPlan) (oraclegen.Item, bool) {
	if plan != nil {
		return activateWithAbilityStack(reg, f, name, req, idx, prefix, mana, cost, zone, slots, restrictions, plan)
	}
	preludes := withTokenCostPrelude(reg, cost, append([]conditionPrelude{{}}, restrictions...))
	// Stack slots are served here by a prelude cast the scenario holds at
	// this step's priority; every other template family keeps the plain
	// fixtures, whose stack slots stay the caller's own precast.
	for _, fx := range oraclegen.FixturesServingStack(reg, slots, stackTargets...) {
		for _, pre := range preludes {
			it, ok := activateWithFixture(reg, f, name, req, idx, prefix, mana, cost, zone, fx, pre, slots, nil)
			if ok {
				return it, true
			}
		}
	}
	return oraclegen.Item{}, false
}

// activateWithFixture builds and settles one fixture with one restriction
// prelude applied. A non-nil plan targets the ability's stack slots at the
// pending probe ability the plan's prelude leaves on the stack.
func activateWithFixture(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement, idx int, prefix, mana, cost, zone string, fx oraclegen.Fixture, pre conditionPrelude, slots []oraclegen.Slot, plan *abilityStackPlan) (oraclegen.Item, bool) {
	abilityIndex := idx
	p0 := *fx.P0()
	if plan != nil {
		plan.setup(&p0)
	}
	switch zone {
	case "hand":
		p0.Hand = appendFixtureUnique(p0.Hand, name)
	case "graveyard":
		p0.Graveyard = appendFixtureUnique(p0.Graveyard, name)
	default:
		p0.Battlefield = appendFixtureUnique(p0.Battlefield, name)
	}
	x := activationX(cost, saXMin(f.Abilities[abilityIndex]))
	addActivationCostFixtures(&p0, name, cost, x)
	addActivationCounterFixtures(&p0, f, name, cost)
	etbCostGuardFixtures(&p0, f, cost, x)
	if need := loyaltySetupCounters(f, f.Abilities[idx], zone); need > 0 {
		p0 = oraclegen.WithCounters(p0, name, "LOYALTY", int32(need))
	}
	if zone == "battlefield" {
		// A source whose printed toughness is zero or less dies the moment
		// it is placed (Marketback Walker is a 0/0 whose X +1/+1 entry
		// counters read X=0 at setup); seed one +1/+1 counter per missing
		// point of toughness so the activation can be probed at all.
		if t, ok := printedToughness(f); ok && t <= 0 {
			p0 = oraclegen.WithCounters(p0, name, "P1P1", int32(1-t))
		}
	}
	p0, restrictSteps := applyActivationPrelude(p0, name, pre)
	// The fixture's own prelude (a token made, an Aura attached) runs first
	// in the main phase, then the restriction's steps, then the fixture's
	// combat: a target filter naming an attacking creature ("another target
	// attacking creature you control") has that creature declared attacking
	// before the activation, which then happens in combat.
	fxPre, combat := fx.Prelude(), fx.CombatSteps()
	costAttach := sacAttachSteps(name, cost, zone)
	sourceAttach := auraSourceAttach(f, name, zone, &p0)
	prelude := make([]oraclegen.Step, 0, len(fxPre)+len(restrictSteps)+len(costAttach)+len(sourceAttach)+len(combat)+1)
	if isLoyaltyCost(cost) {
		// CR 606.3: a loyalty ability needs an empty stack. The setup can
		// leave the source's own entry trigger pending (Oko, Lorwyn Liege's
		// transform enters trigger); one resolve step empties it.
		prelude = append(prelude, oraclegen.Step{Op: "resolve"})
	}
	prelude = append(prelude, sourceAttach...)
	prelude = append(prelude, fxPre...)
	prelude = append(prelude, restrictSteps...)
	prelude = append(prelude, costAttach...)
	if plan != nil {
		// The ability prelude leaves the probe ability pending on the stack;
		// the ability under test (the last activate step) targets it.
		prelude = append(prelude, plan.steps()...)
	}
	prelude = append(prelude, combat...)
	setupBackFace(&p0, name, req)
	steps := make([]oraclegen.Step, 0, len(prelude)+1)
	steps = append(steps, prelude...)
	targets := fx.Targets()
	if plan != nil {
		targets = plan.targets
	}
	steps = append(steps, oraclegen.Step{
		Op: "activate", Seat: 0, Card: "p0:" + name,
		Mana: mana, Targets: targets, AbilityIndex: &abilityIndex,
		Answers: activationXAnswers(cost, x),
	})
	sc := oraclegen.Scenario{
		Setup:        map[string]oraclegen.Seat{"p0": p0, "p1": *fx.P1()},
		SetupAnswers: oraclegen.OpeningHandAnswers(f),
		Steps:        steps,
	}
	oraclegen.Baseline(sc.Setup, f)
	n, res, ok := oraclegen.Settle(reg, sc)
	if !ok {
		return oraclegen.Item{}, false
	}
	// A mana ability leaves the stack empty after the activate step. A
	// loyalty-cost ability with the Mana API (Chandra, Flameshaper's
	// "[+2]: Add {R}{R}{R}") is NOT a mana ability (CR 605.1b) and does
	// use the stack, so the resolve count follows the engine's own stack
	// rather than the requirement's sub-family.
	if len(res.Snapshots) < len(prelude)+2 || len(res.Snapshots[len(prelude)+1].Stack) == 0 {
		n = 0
	}
	for i := 0; i < n; i++ {
		sc.Steps = append(sc.Steps, oraclegen.Step{Op: "resolve"})
	}
	// The fixture over-offers targets; rewrite the activate step to
	// exactly gorge's picks and verify the rewrite replays cleanly.
	sc, targetSteps := oraclegen.ChooseTargets(sc, res.Decisions)
	res, ok = oraclegen.PlaysThrough(reg, sc)
	if !ok {
		return oraclegen.Item{}, false
	}
	// A hidden library search gorge's fallback declined: re-run taking the
	// search's first eligible card, so XMage's mandatory TargetCardInLibrary
	// ask is answered with a card instead of the [target_skip] it rejects.
	forced := false
	if search, changed := oraclegen.SearchPicks(sc, res.Decisions); changed {
		if res2, ok2 := oraclegen.PlaysThrough(reg, search); ok2 {
			sc, res = search, res2
			forced = true
		}
	}
	it := oraclegen.NewLevelBItem(name, req.Key, ActivateAbility.Version, []string{"602.2"}, sc)
	it.XAnswers = oraclegen.XAnswersForScenario(res, sc, oraclegen.ModeNumbers(f), targetSteps)
	// A forced first-card pick (SearchPicks' re-run of a declined exile-look
	// ask): XMage poses that ask on its target queue, so the pick's answer is
	// retargeted from the choice form.
	if forced {
		it.XAnswers = oraclegen.RetargetForcedLookPicks(it.XAnswers, res.Decisions)
	}
	if len(it.XAnswers) == 0 {
		it.XAnswers = make([][]oraclegen.XAnswer, len(sc.Steps))
	}
	activateStep := activateStepIndex(sc.Steps)
	// XMage consumes answers FIFO. Cost choices are asked while paying the
	// activation, before any choices made by the resolving ability (such as
	// the colour of mana it produces).
	costAnswerStart := len(it.XAnswers[activateStep])
	addActivationCostAnswers(it.XAnswers, activateStep, cost, name, res.Decisions, x)
	costAnswers := append([]oraclegen.XAnswer(nil), it.XAnswers[activateStep][costAnswerStart:]...)
	it.XAnswers[activateStep] = append(costAnswers, it.XAnswers[activateStep][:costAnswerStart]...)
	// The hoist above puts the cost picks first; a setup permanent's as-enters
	// choice must still lead the step, or XMage's dialog eats the cost answer.
	oraclegen.HoistSetupChoices(it.XAnswers, activateStep)
	dropCostCompound(it.XAnswers, activateStep, res.Decisions)
	dropUnproducedManaColours(it.XAnswers, activateStep, res)
	it.XAnswers = scriptPreludeSacrifice(it.XAnswers, prelude, len(sc.Steps))
	it.XAbility = make([]string, len(sc.Steps))
	copy(it.XAbility[len(fxPre):], pre.xability)
	it.XAbility[activateStep] = prefix
	if comboPrefix, ok := comboManaColourPrefix(f.Abilities[idx], cost, activateStep, res); ok {
		it.XAbility[activateStep] = comboPrefix
		dropColourChoices(it.XAnswers, activateStep)
	}
	// A waterbend-cost activation: the tap-helpers ask the routing dropped
	// was the only scripted answer for asks XMage never poses; whatever the
	// ability itself asks (an unfilled "up to" target, the Waterbend<X>
	// announce, a "you may reveal" pick) is re-scripted here.
	addWaterbendFollowups(it.XAnswers, activateStep, cost, f.Abilities[idx].Params["SpellDescription"],
		sc.Steps[activateStep].Targets, fx.Omitted(), slots)
	return it, true
}

// loyaltySetupCounters is how many loyalty counters the source needs at
// setup: a planeswalker on the battlefield carries only its printed loyalty
// (the scenario mints it there; no engine adds counters for it), so the
// printed count is the floor and a minus ability above it (or an ultimate
// gated on "N or more loyalty counters among <type>s you control", Jace,
// Reality Sculptor's CheckSVar$ Y | SVarCompare$ GE25) raises it. A card
// without a numeric printed loyalty or activated away from the battlefield
// (a hand/planeswalker channel) needs no counters.
func loyaltySetupCounters(f *cards.Face, sa *cards.SA, zone string) int {
	if zone != "battlefield" {
		return 0
	}
	printed, err := strconv.Atoi(strings.TrimSpace(f.Loyalty))
	if err != nil {
		return 0
	}
	need := 0
	for _, tok := range costTokens(sa.ParamStr(cards.PKCost)) {
		if strings.HasPrefix(tok, "SubCounter<") && loyaltyCounter(tok) {
			n, _ := strconv.Atoi(tok[len("SubCounter<"):strings.IndexByte(tok, '/')])
			need = max(need, n)
		}
	}
	// The setup ADDS counters to the placed source, whose placement already
	// gave it the printed loyalty: only the shortfall over the printed
	// loyalty is seeded (a gate wants the source topped up to it, CR 606.6).
	return max(0, max(need, loyaltyGate(f, sa))-printed)
}

// printedToughness is the face's printed toughness as an integer; ok is false
// for a face without an integer "P/T" (an "X/X" or "*/*" creature).
func printedToughness(f *cards.Face) (int, bool) {
	_, tough, ok := strings.Cut(strings.TrimSpace(f.PT), "/")
	if !ok {
		return 0, false
	}
	t, err := strconv.Atoi(strings.TrimSpace(tough))
	if err != nil {
		return 0, false
	}
	return t, true
}

// isLoyaltyCost reports whether a Cost$ carries a loyalty counter part
// (AddCounter<N/LOYALTY> or SubCounter<N/LOYALTY>): the ability is a
// planeswalker's loyalty ability (CR 606.3).
func isLoyaltyCost(cost string) bool {
	for _, tok := range costTokens(cost) {
		if loyaltyCounter(tok) {
			return true
		}
	}
	return false
}

// auraSourceAttach brings a battlefield Aura's source on attached, so the
// Aura's own activated ability (offered only while attached) is offered at
// the probe. An unattached Aura placed by the setup is swept to the graveyard
// by the CR 704.5m state-based action before the first priority, so the
// source enters by a real cast onto a bearer the prelude places instead: the
// card moves to the hand and the prelude casts it (the same shape
// enchantedCandidates uses), which attaches it as it enters. An Aura
// activated from another zone, an unpriceable mana cost, or an ability of a
// non-Aura adds none.
func auraSourceAttach(f *cards.Face, name, zone string, p0 *oraclegen.Seat) []oraclegen.Step {
	if zone != "battlefield" || !oraclegen.HasType(f, "Aura") {
		return nil
	}
	mana, why := oraclegen.PoolFor(f.ManaCost)
	if why != "" {
		return nil
	}
	const bearer = "Hill Giant"
	p0.Battlefield = appendFixtureUnique(p0.Battlefield, bearer)
	for i, n := range p0.Battlefield {
		if n == name {
			p0.Battlefield = append(p0.Battlefield[:i], p0.Battlefield[i+1:]...)
			break
		}
	}
	p0.Hand = appendFixtureUnique(p0.Hand, name)
	return []oraclegen.Step{
		{Op: "cast", Seat: 0, Card: "p0:" + name, Mana: mana, Targets: []string{"p0:" + bearer}},
		{Op: "resolve"},
	}
}

// loyaltyGate reads an activation restriction counting loyalty counters
// among permanents of the source's own type (CheckSVar$ <V> |
// SVarCompare$ GE<n>, V = Count$Valid <Type>.YouCtrl$CardCounters.LOYALTY)
// and returns n, or 0 when the ability has no such gate.
func loyaltyGate(f *cards.Face, sa *cards.SA) int {
	cmp, ok := strings.CutPrefix(strings.TrimSpace(sa.Params["SVarCompare"]), "GE")
	if !ok {
		return 0
	}
	n, err := strconv.Atoi(cmp)
	if err != nil {
		return 0
	}
	body := strings.TrimSpace(f.SVars[strings.TrimSpace(sa.Params["CheckSVar"])])
	valid, ok := strings.CutPrefix(body, "Count$Valid ")
	if !ok {
		return 0
	}
	filter, counted, ok := strings.Cut(valid, "$")
	if !ok || !strings.EqualFold(counted, "CardCounters.LOYALTY") {
		return 0
	}
	typ := strings.SplitN(strings.TrimSpace(filter), ".", 2)[0]
	if !oraclegen.HasType(f, typ) {
		return 0
	}
	return n
}

// attackedThisTurn reports whether a target filter demands a creature that
// attacked this turn.
func attackedThisTurn(filter string) bool {
	for _, part := range strings.FieldsFunc(filter, func(r rune) bool { return r == '.' || r == '+' || r == ',' }) {
		if strings.EqualFold(strings.TrimSpace(part), "attackedThisTurn") {
			return true
		}
	}
	return false
}

// activateStepIndex returns the index of the scenario's PROBE activate step.
// A probe with an activation prelude (a Class level-up) emits the prelude's
// activate steps first, so the LAST activate step is the ability under test;
// the cost answers and mana-colour drops must target it, not the prelude.
func activateStepIndex(steps []oraclegen.Step) int {
	found := 0
	for i := range steps {
		if steps[i].Op == "activate" {
			found = i
		}
	}
	return found
}

// costTokens splits a Forge cost string on whitespace, keeping a token's
// `<...>` payload together: Sac<1/CARDNAME/this creature> and
// tapXType<Any/Creature.Other+withTotalPowerGE1> carry spaces a naive
// Fields split would break.
func costTokens(cost string) []string { return levelb.CostTokens(cost) }

// isNumericBracket reports whether tok is `Head<N>` with a literal N.
func isNumericBracket(tok string) bool {
	i := strings.IndexByte(tok, '<')
	if i < 0 || !strings.HasSuffix(tok, ">") {
		return false
	}
	_, err := strconv.Atoi(tok[i+1 : len(tok)-1])
	return err == nil
}

func selfZoneCost(tok string) bool {
	payload, ok := bracketPayload(tok)
	if !ok {
		return false
	}
	parts := strings.Split(payload, "/")
	return len(parts) >= 2 && parts[0] == "1" &&
		(strings.EqualFold(parts[1], "CARDNAME") || strings.EqualFold(parts[1], "NICKNAME"))
}

// graveyardCreatureCost recognises ExileFromGrave<1/Creature.Other[/text]>:
// one creature card from the graveyard other than the source, which the
// fixture supplies as Grizzly Bears.
func graveyardCreatureCost(tok string) bool {
	payload, ok := bracketPayload(tok)
	if !ok {
		return false
	}
	parts := strings.Split(payload, "/")
	return len(parts) >= 2 && parts[0] == "1" && strings.EqualFold(parts[1], "Creature.Other")
}

func bracketPayload(tok string) (string, bool) {
	i := strings.IndexByte(tok, '<')
	if i < 0 || !strings.HasSuffix(tok, ">") {
		return "", false
	}
	return tok[i+1 : len(tok)-1], true
}

// addActivationCostAnswers scripts XMage's cost selector with the same
// deterministic fixture objects used by the engine-side payment path.
func addActivationCostAnswers(answers [][]oraclegen.XAnswer, step int, cost, name string, decisions []rules.OracleDecision, x ...int) {
	xv := activationX(cost, x...)
	if step < 0 || step >= len(answers) {
		return
	}
	var tokenPicks []string
	for _, tok := range costTokens(cost) {
		head := tok
		if i := strings.IndexByte(tok, '<'); i >= 0 {
			head = tok[:i]
		}
		var picks []string
		switch head {
		case "Discard":
			if selfZoneCost(tok) {
				break
			}
			picks = discardCostPlacement(tok)
		case "ExileFromGrave", "ExileCtrlOrGrave", "CollectEvidence", "Exile":
			picks = activationCostFixturesX(tok, xv)
			if len(picks) == 0 {
				if n, ok := namedSelfCount(tok, name, xv); ok {
					for i := 0; i < n; i++ {
						picks = append(picks, name)
					}
				}
			}
		case "Return":
			// The engine's observed returncost pick is authoritative (a broad
			// "tapped creature" filter can reach the fixture creature); the
			// catalogue fixture is the fallback when the ask was answered
			// before the decisions were recorded.
			observed := false
			for _, d := range decisions {
				if d.Step != step || d.Seat != 0 || d.Kind != "choose_n" {
					continue
				}
				for i, kind := range d.PickKinds {
					if kind != "returncost" {
						continue
					}
					observed = true
					if i < len(d.Picks) {
						picks = append(picks, observedCostPick(d, i))
					}
				}
			}
			if !observed {
				if card := returnCreatureFixture(tok); card != "" {
					picks = []string{card}
				}
			}
		case "Sac":
			// The engine's observed pick is authoritative. A broad filter can
			// include the ability's source, so a catalogue fixture is not
			// necessarily the permanent the payment path actually sacrificed.
			observed := false
			for _, d := range decisions {
				if d.Step != step || d.Seat != 0 || d.Kind != "choose_n" {
					continue
				}
				for i, kind := range d.PickKinds {
					if kind != "sacrifice" {
						continue
					}
					observed = true
					if i < len(d.Picks) {
						picks = append(picks, observedSacPick(tok, d, i))
					}
				}
			}
			// A self-sacrifice is usually a singleton with no ask. For cases
			// with no observed sacrifice decision, use the deterministic fixture.
			if !observed {
				if cards, ok := sacFilterFixtures(tok, xv); ok {
					picks = cards
				} else if card, ok := sacAttachedFixture(tok); ok {
					picks = []string{card}
				} else if needs, ok := tokenCostNeeds(tok); ok {
					picks = tokenCostAnswerNames(needs)
				}
			}
		}
		kind := costAnswerKind(cost, head)
		for _, pick := range picks {
			if isTokenPick(pick) {
				// Token picks repeat (one answer per token), so they
				// bypass the dedupe below.
				tokenPicks = append(tokenPicks, pick)
				continue
			}
			present := false
			for _, answer := range answers[step] {
				if answer.Seat == 0 && answer.Kind == kind && strings.EqualFold(answer.Value, pick) {
					present = true
					break
				}
			}
			if !present {
				answers[step] = append(answers[step], oraclegen.XAnswer{Seat: 0, Kind: kind, Value: pick})
			}
		}
	}
	answers[step] = appendTokenAnswers(answers[step], 0, tokenPicks...)
	addTapXTypeAnswers(answers, step, cost, decisions)
}

// dropUnproducedManaColours removes the activate step's colour choice when
// the activation produced no coloured mana. An any-colour mana ability whose
// amount resolved to zero (Three Tree City with no creature of the chosen
// type) poses no colour dialog in XMage, and a queued colour is then consumed
// by an unrelated dialog (XMage throws "Choice key [White] not found"). The
// pool after the activate step is the ground truth: a colour choice is
// scripted only when the mana it names was actually added. Setup ETB colour
// answers use the distinct setup_choice kind and are never stripped here.
func dropUnproducedManaColours(answers [][]oraclegen.XAnswer, step int, res rules.OracleResult) {
	if step < 0 || step >= len(answers) || len(res.Snapshots) <= step+1 {
		return
	}
	if strings.ContainsAny(res.Snapshots[step+1].Players[0].Pool, "WUBRG") {
		return
	}
	kept := answers[step][:0]
	for _, a := range answers[step] {
		if a.Kind == "choice" && isColourName(a.Value) {
			continue
		}
		kept = append(kept, a)
	}
	answers[step] = kept
}

// isColourName reports whether v is an XMage colour-choice value.
func isColourName(v string) bool {
	switch v {
	case "White", "Blue", "Black", "Red", "Green":
		return true
	}
	return false
}

// addActivationCostFixtures supplies the explicit cards needed by supported
// non-mana cost tokens. These are not choices: the shared runner makes the
// deterministic legal selection, and XAnswersForScenario records that same
// choice for XMage.
func addActivationCostFixtures(p0 *oraclegen.Seat, name, cost string, x ...int) {
	xv := activationX(cost, x...)
	for _, tok := range costTokens(cost) {
		head := tok
		if i := strings.IndexByte(tok, '<'); i >= 0 {
			head = tok[:i]
		}
		switch head {
		case "Discard":
			if selfZoneCost(tok) {
				break
			}
			for _, name := range discardCostPlacement(tok) {
				p0.Hand = appendFixtureUnique(p0.Hand, name)
			}
		case "ExileFromGrave", "ExileCtrlOrGrave", "CollectEvidence":
			if n, ok := namedSelfCount(tok, name, xv); ok {
				// The filter names the source itself (Say Its Name): the
				// payment's fodder is n copies of the card, which appendFixtureUnique
				// would collapse to one.
				for i := 0; i < n; i++ {
					p0.Graveyard = append(p0.Graveyard, name)
				}
				continue
			}
			for _, fixture := range activationCostFixturesX(tok, xv) {
				p0.Graveyard = appendFixtureUnique(p0.Graveyard, fixture)
			}
		case "Exile":
			for _, name := range activationCostFixturesX(tok, xv) {
				p0.Battlefield = appendFixtureUnique(p0.Battlefield, name)
			}
		case "Return":
			if card := returnCreatureFixture(tok); card != "" {
				p0.Battlefield = appendFixtureUnique(p0.Battlefield, card)
				p0.Tapped = appendFixtureUnique(p0.Tapped, card)
			}
		case "Sac":
			// The fixture table is the single authority; a self-sacrifice
			// places nothing (the source is already on the battlefield). An
			// attached Aura/Equipment is placed here and attached by
			// sacAttachSteps in the prelude.
			if card, ok := sacAttachedFixture(tok); ok {
				p0.Battlefield = appendFixtureUnique(p0.Battlefield, card)
			} else if cards, ok := sacFilterFixtures(tok, xv); ok {
				for _, card := range cards {
					p0.Battlefield = appendFixtureUnique(p0.Battlefield, card)
				}
			}
		}
	}
	addTapXTypeFixtures(p0, cost, xv)
	keywordCostFixtures(p0, cost)
}

// loyaltyCounter reports whether tok is an AddCounter<N/LOYALTY> or
// SubCounter<N/LOYALTY> with a literal N: a planeswalker's own loyalty cost.
func loyaltyCounter(tok string) bool {
	i := strings.IndexByte(tok, '<')
	if i < 0 || !strings.HasSuffix(tok, ">") {
		return false
	}
	fields := strings.Split(tok[i+1:len(tok)-1], "/")
	if len(fields) != 2 || !strings.EqualFold(strings.TrimSpace(fields[1]), "LOYALTY") {
		return false
	}
	_, err := strconv.Atoi(fields[0])
	return err == nil
}

// saXMin is the ability's XMin<N> floor, the lowest legal announced X (a Sac<X/>
// or ExileFromGrave<X/> count pays it), 0 when the ability names none.
func saXMin(sa *cards.SA) int {
	n, err := strconv.Atoi(strings.TrimSpace(sa.ParamStr(cards.PKXMin)))
	if err != nil || n < 1 {
		return 0
	}
	return n
}

// costHead names an offending token for a skip reason, folding a bracketed
// payload to an ellipsis so a census groups by head rather than by value.
func costHead(tok string) string {
	if i := strings.IndexByte(tok, '<'); i >= 0 {
		return tok[:i] + "<...>"
	}
	return tok
}
