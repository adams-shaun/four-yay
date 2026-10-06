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
	pool, gap := activationCostIn(sa.ParamStr(cards.PKCost), zone)
	if gap != "" {
		return oraclegen.Item{}, &oraclegen.Skip{Card: name, Reason: "activate cost gap: " + gap}
	}
	slots := oraclegen.AbilitySlotSpecs(f, sa)
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
	it, ok := activateWith(reg, f, name, req, idx, prefix, pool, sa.ParamStr(cards.PKCost), zone, slots)
	if !ok {
		if oraclegen.HasType(f, "Aura") {
			// An Aura's ability is offered only while it is attached; this
			// template has no attach prelude, so name that cause.
			return oraclegen.Item{}, &oraclegen.Skip{Card: name,
				Reason: fmt.Sprintf("activate no fixture gorge can activate (Aura needs an attach prelude; targets %v)", filterStrings(slots))}
		}
		return oraclegen.Item{}, &oraclegen.Skip{Card: name,
			Reason: fmt.Sprintf("activate no fixture gorge can activate (targets %v)", filterStrings(slots))}
	}
	return it, nil
}

// activateWith tries every fixture for the ability's target plan and returns
// the named level-B item.
func activateWith(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement, idx int, prefix, mana, cost, zone string, slots []oraclegen.Slot) (oraclegen.Item, bool) {
	for _, fx := range oraclegen.Fixtures(reg, slots) {
		abilityIndex := idx
		p0 := *fx.P0()
		switch zone {
		case "hand":
			p0.Hand = appendFixtureUnique(p0.Hand, name)
		case "graveyard":
			p0.Graveyard = appendFixtureUnique(p0.Graveyard, name)
		default:
			p0.Battlefield = appendFixtureUnique(p0.Battlefield, name)
		}
		addActivationCostFixtures(&p0, cost)
		addActivationCounterFixtures(&p0, name, cost)
		if extra := loyaltyHeadroom(f, f.Abilities[idx]); extra > 0 {
			p0 = oraclegen.WithCounters(p0, name, "LOYALTY", int32(extra))
		}
		setupBackFace(&p0, name, req)
		sc := oraclegen.Scenario{
			Setup:        map[string]oraclegen.Seat{"p0": p0, "p1": *fx.P1()},
			SetupAnswers: oraclegen.OpeningHandAnswers(f),
			Steps: []oraclegen.Step{{
				Op: "activate", Seat: 0, Card: "p0:" + name,
				Mana: mana, Targets: fx.Targets(), AbilityIndex: &abilityIndex,
				Answers: activationXAnswers(cost),
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
		// A hidden library search gorge's fallback declined: re-run taking the
		// search's first eligible card, so XMage's mandatory TargetCardInLibrary
		// ask is answered with a card instead of the [target_skip] it rejects.
		if search, changed := oraclegen.SearchPicks(sc, res.Decisions); changed {
			if res2, ok2 := oraclegen.PlaysThrough(reg, search); ok2 {
				sc, res = search, res2
			}
		}
		it := oraclegen.NewLevelBItem(name, req.Key, ActivateAbility.Version, []string{"602.2"}, sc)
		it.XAnswers = oraclegen.XAnswersForScenario(res, sc, oraclegen.ModeNumbers(f), targetSteps)
		if len(it.XAnswers) == 0 {
			it.XAnswers = make([][]oraclegen.XAnswer, len(sc.Steps))
		}
		addActivationCostAnswers(it.XAnswers, activateStepIndex(sc.Steps), cost, res.Decisions)
		dropUnproducedManaColours(it.XAnswers, activateStepIndex(sc.Steps), res)
		addSetupColourAnswers(it.XAnswers, res.Decisions)
		it.XAbility = make([]string, len(sc.Steps))
		it.XAbility[activateStepIndex(sc.Steps)] = prefix
		return it, true
	}
	return oraclegen.Item{}, false
}

// loyaltyHeadroom is how many loyalty counters the source needs at setup
// beyond its printed loyalty for the ability to be activatable: a loyalty
// cost can't be paid with too few counters (CR 606.6), so a minus ability
// above the printed loyalty needs the difference, and an ultimate gated on
// "N or more loyalty counters among <type>s you control" (Jace, Reality
// Sculptor's CheckSVar$ Y | SVarCompare$ GE25 over
// Count$Valid Jace.YouCtrl$CardCounters.LOYALTY) needs N on the source when
// the source is of that type. Zero for a card without numeric printed
// loyalty.
func loyaltyHeadroom(f *cards.Face, sa *cards.SA) int {
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
	need = max(need, loyaltyGate(f, sa))
	return max(0, need-printed)
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

func selfZoneCost(tok string) bool {
	payload, ok := bracketPayload(tok)
	if !ok {
		return false
	}
	parts := strings.Split(payload, "/")
	return len(parts) >= 2 && parts[0] == "1" && strings.EqualFold(parts[1], "CARDNAME")
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
			if selfZoneCost(tok) {
				break
			}
			payload, _ := bracketPayload(tok)
			if strings.Contains(strings.ToLower(payload), "legendary") {
				picks = []string{"Ajani, Caller of the Pride"}
			} else {
				picks = discardCostFixtures(tok)
				if len(picks) == 0 {
					picks = []string{"Wastes"}
				}
			}
		case "ExileFromGrave", "ExileCtrlOrGrave", "CollectEvidence", "Exile":
			picks = activationCostFixturesX(tok, activationX(cost))
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
						picks = append(picks, d.Picks[i])
					}
				}
			}
			// A self-sacrifice is usually a singleton with no ask. For cases
			// with no observed sacrifice decision, use the deterministic fixture.
			if !observed {
				if card, ok := sacFilterFixture(tok); ok {
					picks = []string{card}
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

// dropUnproducedManaColours removes the activate step's colour choice when
// the activation produced no coloured mana. An any-colour mana ability whose
// amount resolved to zero (Three Tree City with no creature of the chosen
// type) poses no colour dialog in XMage, and a queued colour is then consumed
// by an unrelated dialog (XMage throws "Choice key [White] not found"). The
// pool after the activate step is the ground truth: a colour choice is
// scripted only when the mana it names was actually added. Called before
// addSetupColourAnswers, so a setup-placed permanent's ETB colour (which no
// activation produces) is never stripped here.
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

// addSetupColourAnswers scripts the colour gorge chose as a setup-placed
// permanent entered ("As ~ enters, choose a color": Crossroads Village,
// Heraldic Banner). XMage poses the same ETB colour dialog when it puts the
// card on the battlefield at game start, before any step, and answers it at
// random when its choice queue is empty. The queue is first-in first-out and
// every step's answers are queued before the game starts, so the colour goes
// FIRST on step 0's answers.
func addSetupColourAnswers(answers [][]oraclegen.XAnswer, decisions []rules.OracleDecision) {
	if len(answers) == 0 {
		return
	}
	var setup []oraclegen.XAnswer
	for _, d := range decisions {
		if d.Step >= 0 || d.Resume != "etb" || len(d.Picks) != 1 || len(d.PickKinds) != 1 || d.PickKinds[0] != "color" {
			continue
		}
		setup = append(setup, oraclegen.XAnswer{Seat: d.Seat, Kind: "choice", Value: d.Picks[0]})
	}
	answers[0] = append(setup, answers[0]...)
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
			if selfZoneCost(tok) {
				break
			}
			payload, _ := bracketPayload(tok)
			if strings.Contains(strings.ToLower(payload), "legendary") {
				p0.Hand = appendFixtureUnique(p0.Hand, "Ajani, Caller of the Pride")
			} else {
				for _, name := range discardCostFixtures(tok) {
					p0.Hand = appendFixtureUnique(p0.Hand, name)
				}
				if len(discardCostFixtures(tok)) == 0 {
					p0.Hand = appendFixtureUnique(p0.Hand, "Wastes")
				}
			}
		case "ExileFromGrave", "ExileCtrlOrGrave", "CollectEvidence":
			for _, name := range activationCostFixturesX(tok, activationX(cost)) {
				p0.Graveyard = appendFixtureUnique(p0.Graveyard, name)
			}
		case "Exile":
			for _, name := range activationCostFixturesX(tok, activationX(cost)) {
				p0.Battlefield = appendFixtureUnique(p0.Battlefield, name)
			}
		case "Sac":
			// The fixture table is the single authority; a self-sacrifice
			// places nothing (the source is already on the battlefield).
			if card, ok := sacFilterFixture(tok); ok {
				p0.Battlefield = appendFixtureUnique(p0.Battlefield, card)
			}
		}
	}
	addTapXTypeFixtures(p0, cost)
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

// costHead names an offending token for a skip reason, folding a bracketed
// payload to an ellipsis so a census groups by head rather than by value.
func costHead(tok string) string {
	if i := strings.IndexByte(tok, '<'); i >= 0 {
		return tok[:i] + "<...>"
	}
	return tok
}
