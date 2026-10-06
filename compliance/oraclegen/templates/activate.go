// Level-B activate template (spec
// docs/superpowers/specs/2026-10-05-compliance-level-b.md section 2.1; ticket
// L5). It serves the activate.battlefield and activate.mana requirements: the
// card on p0's battlefield, the ability's targets from the ordinary fixture
// cross product, one `activate` step naming the ability by IR index, and a
// resolve for a non-mana ability.
//
// v1 cost tokens: mana, T, Q, PayLife<n>, one-card Discard, Sac (self or
// filtered other permanent), Exile<1/CARDNAME>, supported tapXType fixture
// shapes, and a source loyalty AddCounter/SubCounter. Anything else is a cost gap. The XMage
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
	return sub == "activate.battlefield" || sub == "activate.mana"
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
	pool, gap := activationCost(sa.ParamStr(cards.PKCost))
	if gap != "" {
		return oraclegen.Item{}, &oraclegen.Skip{Card: name, Reason: "activate cost gap: " + gap}
	}
	slots := oraclegen.AbilitySlotSpecs(f, sa)
	it, ok := activateWith(reg, f, name, req, idx, prefix, pool, sa.ParamStr(cards.PKCost), slots)
	if !ok {
		return oraclegen.Item{}, &oraclegen.Skip{Card: name,
			Reason: fmt.Sprintf("activate no fixture gorge can activate (targets %v)", filterStrings(slots))}
	}
	return it, nil
}

// activateWith tries every fixture for the ability's target plan and returns
// the named level-B item.
func activateWith(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement, idx int, prefix, mana, cost string, slots []oraclegen.Slot) (oraclegen.Item, bool) {
	for _, fx := range oraclegen.Fixtures(reg, slots) {
		abilityIndex := idx
		p0 := *fx.P0()
		p0.Battlefield = appendFixtureUnique(p0.Battlefield, name)
		addActivationCostFixtures(&p0, cost)
		setupBackFace(&p0, name, req)
		sc := oraclegen.Scenario{
			Setup:        map[string]oraclegen.Seat{"p0": p0, "p1": *fx.P1()},
			SetupAnswers: oraclegen.OpeningHandAnswers(f),
			Steps: []oraclegen.Step{{
				Op: "activate", Seat: 0, Card: "p0:" + name,
				Mana: mana, Targets: fx.Targets(), AbilityIndex: &abilityIndex,
			}},
		}
		oraclegen.Baseline(sc.Setup, f)
		n, res, ok := oraclegen.Settle(reg, sc)
		if !ok {
			continue
		}
		// A mana ability leaves the stack empty after the activate step. A
		// loyalty-cost ability with the Mana API (Chandra, Flameshaper's
		// "[+2]: Add {R}{R}{R}") is NOT a mana ability (CR 605.1b) and does
		// use the stack, so the resolve count follows the engine's own stack
		// rather than the requirement's sub-family.
		if len(res.Snapshots) < 2 || len(res.Snapshots[1].Stack) == 0 {
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
			continue
		}
		it := oraclegen.NewLevelBItem(name, req.Key, ActivateAbility.Version, []string{"602.2"}, sc)
		it.XAnswers = oraclegen.XAnswersForScenario(res, sc, oraclegen.ModeNumbers(f), targetSteps)
		if len(it.XAnswers) == 0 {
			it.XAnswers = make([][]oraclegen.XAnswer, len(sc.Steps))
		}
		addActivationCostAnswers(it.XAnswers, activateStepIndex(sc.Steps), cost, res.Decisions)
		it.XAbility = make([]string, len(sc.Steps))
		it.XAbility[activateStepIndex(sc.Steps)] = prefix
		return it, true
	}
	return oraclegen.Item{}, false
}

// activateStepIndex returns the index of the scenario's activate step. The
// template emits it first today; the helper keeps XAbility parallel to Steps
// if a prelude is ever added.
func activateStepIndex(steps []oraclegen.Step) int {
	for i := range steps {
		if steps[i].Op == "activate" {
			return i
		}
	}
	return 0
}

// costTokens splits a Forge cost string on whitespace, keeping a token's
// `<...>` payload together: Sac<1/CARDNAME/this creature> and
// tapXType<Any/Creature.Other+withTotalPowerGE1> carry spaces a naive
// Fields split would break.
func costTokens(cost string) []string {
	var out []string
	depth, start := 0, -1
	for i, r := range cost {
		switch r {
		case '<':
			depth++
		case '>':
			if depth > 0 {
				depth--
			}
		case ' ', '\t':
			if depth == 0 {
				if start >= 0 {
					out = append(out, cost[start:i])
					start = -1
				}
				continue
			}
		}
		if start < 0 {
			start = i
		}
	}
	if start >= 0 {
		out = append(out, cost[start:])
	}
	return out
}

// isNumericBracket reports whether tok is `Head<N>` with a literal N.
func isNumericBracket(tok string) bool {
	i := strings.IndexByte(tok, '<')
	if i < 0 || !strings.HasSuffix(tok, ">") {
		return false
	}
	_, err := strconv.Atoi(tok[i+1 : len(tok)-1])
	return err == nil
}

// sacSelf reports whether a Sac<...> token sacrifices the source itself:
// Sac<1/CARDNAME> or Sac<1/CARDNAME/this creature>. A Sac<N/Spec> naming any
// other permanent is out of v1's scope (a cost gap).
func sacSelf(tok string) bool {
	i := strings.IndexByte(tok, '<')
	if i < 0 || !strings.HasSuffix(tok, ">") {
		return false
	}
	fields := strings.Split(tok[i+1:len(tok)-1], "/")
	return len(fields) >= 2 && strings.EqualFold(fields[1], "CARDNAME")
}

// discardFixtureSupported covers the two v1 discard selectors: any card and
// a legendary card. Both use a single card, so one deterministic hand fixture
// is sufficient and the engine/XMage answer translation selects it.
func discardFixtureSupported(tok string) bool {
	payload, ok := bracketPayload(tok)
	if !ok {
		return false
	}
	parts := strings.Split(payload, "/")
	return len(parts) >= 2 && parts[0] == "1" &&
		(strings.EqualFold(parts[1], "Card") || strings.EqualFold(parts[1], "Card.Legendary"))
}

// sacOtherFixtureSupported recognizes the filtered other-permanent costs the
// template can satisfy with an explicit fixture permanent.
func sacOtherFixtureSupported(tok string) bool {
	payload, ok := bracketPayload(tok)
	if !ok {
		return false
	}
	parts := strings.Split(payload, "/")
	if len(parts) < 2 || parts[0] != "1" {
		return false
	}
	filter := strings.ToLower(parts[1])
	return filter == "creature.other;planeswalker.other" || filter == "artifact.other" || filter == "artifact;land"
}

func selfZoneCost(tok string) bool {
	payload, ok := bracketPayload(tok)
	if !ok {
		return false
	}
	parts := strings.Split(payload, "/")
	return len(parts) >= 2 && parts[0] == "1" && strings.EqualFold(parts[1], "CARDNAME")
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
func addActivationCostAnswers(answers [][]oraclegen.XAnswer, step int, cost string, decisions []rules.OracleDecision) {
	if step < 0 || step >= len(answers) {
		return
	}
	for _, tok := range costTokens(cost) {
		head := tok
		if i := strings.IndexByte(tok, '<'); i >= 0 {
			head = tok[:i]
		}
		var picks []string
		switch head {
		case "Discard":
			payload, _ := bracketPayload(tok)
			if strings.Contains(strings.ToLower(payload), "legendary") {
				picks = []string{"Ajani, Caller of the Pride"}
			} else {
				picks = []string{"Wastes"}
			}
		case "Sac":
			if !sacSelf(tok) {
				payload, _ := bracketPayload(tok)
				if strings.Contains(strings.ToLower(payload), "creature") || strings.Contains(strings.ToLower(payload), "planeswalker") {
					picks = []string{"Llanowar Elves"}
				} else if strings.Contains(strings.ToLower(payload), "artifact") {
					picks = []string{"Ornithopter"}
				} else if strings.Contains(strings.ToLower(payload), "land") {
					picks = []string{"Forest"}
				}
			}
		}
		for _, pick := range picks {
			present := false
			for _, answer := range answers[step] {
				if answer.Seat == 0 && answer.Kind == "choice" && strings.EqualFold(answer.Value, pick) {
					present = true
					break
				}
			}
			if !present {
				answers[step] = append(answers[step], oraclegen.XAnswer{Seat: 0, Kind: "choice", Value: pick})
			}
		}
	}
	addTapXTypeAnswers(answers, step, cost, decisions)
}

// addActivationCostFixtures supplies the explicit cards needed by supported
// non-mana cost tokens. These are not choices: the shared runner makes the
// deterministic legal selection, and XAnswersForScenario records that same
// choice for XMage.
func addActivationCostFixtures(p0 *oraclegen.Seat, cost string) {
	for _, tok := range costTokens(cost) {
		head := tok
		if i := strings.IndexByte(tok, '<'); i >= 0 {
			head = tok[:i]
		}
		switch head {
		case "Discard":
			payload, _ := bracketPayload(tok)
			if strings.Contains(strings.ToLower(payload), "legendary") {
				p0.Hand = appendFixtureUnique(p0.Hand, "Ajani, Caller of the Pride")
			} else {
				p0.Hand = appendFixtureUnique(p0.Hand, "Wastes")
			}
		case "Sac":
			if !sacSelf(tok) {
				payload, _ := bracketPayload(tok)
				if strings.Contains(strings.ToLower(payload), "creature") || strings.Contains(strings.ToLower(payload), "planeswalker") {
					p0.Battlefield = appendFixtureUnique(p0.Battlefield, "Llanowar Elves")
				} else if strings.Contains(strings.ToLower(payload), "artifact") {
					p0.Battlefield = appendFixtureUnique(p0.Battlefield, "Ornithopter")
				} else if strings.Contains(strings.ToLower(payload), "land") {
					p0.Battlefield = appendFixtureUnique(p0.Battlefield, "Forest")
				}
			}
		}
	}
	addTapXTypeFixtures(p0, cost)
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

// costHead names an offending token for a skip reason, folding a bracketed
// payload to an ellipsis so a census groups by head rather than by value.
func costHead(tok string) string {
	if i := strings.IndexByte(tok, '<'); i >= 0 {
		return tok[:i] + "<...>"
	}
	return tok
}
